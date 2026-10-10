package cmd

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/iamartinlong/Argus/internal/application"
	"github.com/iamartinlong/Argus/internal/config"
	"github.com/iamartinlong/Argus/internal/domain"
	appcrypto "github.com/iamartinlong/Argus/internal/infrastructure/crypto"
	"github.com/iamartinlong/Argus/internal/infrastructure/storage"
	"github.com/iamartinlong/Argus/internal/infrastructure/storage/sqlite"
)

var resetPasswordCmd = &cobra.Command{
	Use:   "reset-password",
	Short: "重置用户密码（忘记密码时本地直连数据库，无需 JWT/主密钥）",
	Long: `重置指定用户的登录密码（管理员忘记密码的补救通道）。

前提：能访问 config.yaml 与数据库文件（即本机管理员权限）。
服务运行中亦可执行（SQLite WAL + busy_timeout）；如遇 database is locked，先 Stop-Service 再执行。

示例：
  argus reset-password --config D:\argus\config.yaml --username admin --password "NewPass123"
  argus reset-password --config D:\argus\config.yaml   # 不传密码则随机生成`,
	RunE: func(cmd *cobra.Command, args []string) error {
		username, _ := cmd.Flags().GetString("username")
		password, _ := cmd.Flags().GetString("password")
		return resetPassword(username, password)
	},
}

func init() {
	resetPasswordCmd.Flags().String("username", "admin", "要重置密码的用户名")
	resetPasswordCmd.Flags().String("password", "", "新密码（不传则自动生成随机密码）")
}

func resetPassword(username, password string) error {
	if username == "" {
		return fmt.Errorf("用户名不能为空")
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	db, err := storage.Open(cfg.Storage.Driver, cfg.Storage.DSN)
	if err != nil {
		return err
	}
	defer db.Close()

	ctx := context.Background()
	userStore := sqlite.NewUserStore(db)
	sessionStore := sqlite.NewSessionStore(db)
	auditStore := sqlite.NewAuditStore(db)

	user, err := userStore.GetByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return fmt.Errorf("用户 %q 不存在；若系统尚未初始化，请浏览器打开安装向导（/setup）或执行 argus init-admin", username)
		}
		return err
	}

	// 本地 CLI 模式：AuthService 仅用于吊销会话，JWT 用一次性占位（不签发真实令牌）
	jwtSvc, err := appcrypto.NewJWTService("cli-only-secret-not-used-0123456789abcdef", 15*time.Minute)
	if err != nil {
		return err
	}
	authSvc := application.NewAuthService(userStore, sessionStore, auditStore, jwtSvc,
		application.RealClock{}, 15*time.Minute, 720*time.Hour,
		cfg.Auth.MaxLoginFailures, time.Duration(cfg.Auth.LockoutMinutes)*time.Minute)
	userSvc := application.NewUserService(userStore, auditStore, authSvc, application.RealClock{}, cfg.Auth.MinPasswordLen)

	generated := password == ""
	pwd := password
	if generated {
		pwd, err = randomPassword()
		if err != nil {
			return fmt.Errorf("生成随机密码失败: %w", err)
		}
	}
	actor := &domain.User{ID: "system", Username: "system", Role: domain.RoleAdmin}
	if err := userSvc.ResetPassword(ctx, actor, user, pwd); err != nil {
		return err
	}
	// 随机密码：要求首次登录后立即修改（与 init-admin 一致）
	if generated {
		user.MustChangePassword = true
		if err := userStore.Update(ctx, user); err != nil {
			return err
		}
	}

	fmt.Printf("已重置用户 %s 的密码（角色 %s）\n", user.Username, user.Role)
	if generated {
		fmt.Printf("新密码: %s（首次登录后需立即修改）\n", pwd)
	}
	fmt.Println("该用户的全部登录会话已吊销；已签发的访问令牌最长", cfg.Auth.AccessTokenTTL, "内自然过期")
	return nil
}
