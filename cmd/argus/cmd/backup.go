package cmd

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/iamartinlong/Argus/internal/config"
	"github.com/iamartinlong/Argus/internal/infrastructure/storage/sqlite"
)

func zipAddFile(zw *zip.Writer, name, path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	fh, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: fi.ModTime()})
	if err != nil {
		return err
	}
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer src.Close()
	if _, err := io.Copy(fh, src); err != nil {
		return err
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func fileSize(path string) int64 {
	if fi, err := os.Stat(path); err == nil {
		return fi.Size()
	}
	return -1
}

// verifySQLite 打开数据库并执行完整性检查
func verifySQLite(path string) error {
	db, err := sqlite.Open(path)
	if err != nil {
		return err
	}
	defer db.Close()
	var result string
	if err := db.QueryRowContext(context.Background(), `PRAGMA integrity_check`).Scan(&result); err != nil {
		return err
	}
	if result != "ok" {
		return fmt.Errorf("integrity_check=%q", result)
	}
	return nil
}

// dbFilePath 从 DSN 提取 sqlite 文件路径（支持 file: 前缀）
func dbFilePath(dsn string) string {
	p := dsn
	if strings.HasPrefix(p, "file:") {
		p = strings.TrimPrefix(p, "file:")
		if i := strings.IndexAny(p, "?&"); i >= 0 {
			p = p[:i]
		}
	}
	return p
}

var backupCmd = &cobra.Command{
	Use:   "backup",
	Short: "备份数据库（在线安全快照）到 zip 包",
	RunE: func(cmd *cobra.Command, args []string) error {
		output, _ := cmd.Flags().GetString("output")
		withKey, _ := cmd.Flags().GetBool("with-key")
		cfg, err := config.Load(configPath)
		if err != nil {
			return err
		}
		if cfg.Storage.Driver != "" && cfg.Storage.Driver != "sqlite" {
			return fmt.Errorf("backup 当前仅支持 sqlite 驱动（当前 driver=%s）；postgres 请使用数据库原生备份工具", cfg.Storage.Driver)
		}
		if output == "" {
			output = fmt.Sprintf("argus-backup-%s.zip", time.Now().Format("20060102-150405"))
		}

		dbPath := dbFilePath(cfg.Storage.DSN)
		if dbPath == "" || strings.HasSuffix(dbPath, ":memory:") {
			return fmt.Errorf("当前存储不支持备份（DSN=%q），仅支持文件型 sqlite", cfg.Storage.DSN)
		}
		db, err := sqlite.Open(cfg.Storage.DSN)
		if err != nil {
			return err
		}
		defer db.Close()

		// 在线快照：VACUUM INTO 生成一致性副本
		snapshot := filepath.Join(filepath.Dir(output), ".argus-snapshot-"+time.Now().Format("20060102150405")+".db")
		defer os.Remove(snapshot)
		if _, err := db.ExecContext(cmd.Context(), "VACUUM INTO '"+snapshot+"'"); err != nil {
			return fmt.Errorf("创建快照失败: %w", err)
		}

		f, err := os.Create(output)
		if err != nil {
			return err
		}
		zw := zip.NewWriter(f)
		// manifest
		manifest := map[string]string{
			"app":         "argus",
			"version":     version,
			"driver":      "sqlite",
			"backed_up_at": time.Now().UTC().Format(time.RFC3339),
		}
		mw, err := zw.Create("manifest.json")
		if err != nil {
			return err
		}
		enc := json.NewEncoder(mw)
		enc.SetIndent("", "  ")
		if err := enc.Encode(manifest); err != nil {
			return err
		}
		// 数据库快照
		if err := zipAddFile(zw, "argus.db", snapshot); err != nil {
			return err
		}
		// 主密钥（可选，含敏感信息）
		if withKey {
			key := os.Getenv("ARGUS_MASTER_KEY")
			if key == "" {
				fmt.Println("警告：未检测到 ARGUS_MASTER_KEY 环境变量，master.key 未写入备份包")
			} else {
				kw, err := zw.Create("master.key")
				if err != nil {
					return err
				}
				if _, err := kw.Write([]byte(key + "\n")); err != nil {
					return err
				}
				fmt.Println("警告：备份包内含主密钥，请妥善保管，勿随包分发")
			}
		}
		if err := zw.Close(); err != nil {
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		fmt.Printf("备份完成：%s（%d 字节）\n", output, fileSize(output))
		if !withKey {
			fmt.Println("提示：主密钥未包含在备份中，恢复时需提供原 ARGUS_MASTER_KEY")
		}
		return nil
	},
}

var restoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "从备份 zip 恢复数据库（请先停止服务）",
	RunE: func(cmd *cobra.Command, args []string) error {
		input, _ := cmd.Flags().GetString("input")
		force, _ := cmd.Flags().GetBool("force")
		if input == "" {
			return fmt.Errorf("请指定 --input 备份文件")
		}
		cfg, err := config.Load(configPath)
		if err != nil {
			return err
		}
		if cfg.Storage.Driver != "" && cfg.Storage.Driver != "sqlite" {
			return fmt.Errorf("restore 当前仅支持 sqlite 驱动（当前 driver=%s）", cfg.Storage.Driver)
		}
		dbPath := dbFilePath(cfg.Storage.DSN)
		if dbPath == "" {
			return fmt.Errorf("当前存储不支持恢复（DSN=%q）", cfg.Storage.DSN)
		}
		if !force {
			fmt.Printf("恢复将用备份覆盖 %s（当前文件先备份为 .bak）。请确保服务已停止！\n", dbPath)
			fmt.Print("确认继续？(y/N) ")
			var ans string
			if _, err := fmt.Scanln(&ans); err != nil || !strings.EqualFold(ans, "y") {
				return fmt.Errorf("已取消")
			}
		}

		// 校验并解压备份
		zr, err := zip.OpenReader(input)
		if err != nil {
			return fmt.Errorf("打开备份失败: %w", err)
		}
		defer zr.Close()
		var manifestOK bool
		var dbContent []byte
		for _, zf := range zr.File {
			name := filepath.Clean(zf.Name)
			if strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
				return fmt.Errorf("备份包含非法路径: %s", zf.Name)
			}
			// 防 zip 炸弹：解压大小上限 1GB
			if zf.UncompressedSize64 > 1<<30 {
				return fmt.Errorf("备份条目 %s 解压大小 %d 超过上限 1GB", zf.Name, zf.UncompressedSize64)
			}
			switch name {
			case "manifest.json":
				manifestOK = true
			case "argus.db":
				rc, err := zf.Open()
				if err != nil {
					return err
				}
				content := make([]byte, zf.UncompressedSize64)
				if _, err := io.ReadFull(rc, content); err != nil {
					rc.Close()
					return fmt.Errorf("读取备份数据库失败: %w", err)
				}
				rc.Close()
				dbContent = content
			}
		}
		if !manifestOK {
			return fmt.Errorf("备份包缺少 manifest.json，可能不是有效的 Argus 备份")
		}
		if len(dbContent) == 0 {
			return fmt.Errorf("备份包缺少 argus.db")
		}

		// 恢复前有效性验证：写入临时文件并检查 SQLite 完整性
		tmp := dbPath + ".verify-" + time.Now().Format("20060102150405")
		if err := os.WriteFile(tmp, dbContent, 0o600); err != nil {
			return fmt.Errorf("写入临时验证文件失败: %w", err)
		}
		defer os.Remove(tmp)
		if err := verifySQLite(tmp); err != nil {
			return fmt.Errorf("备份数据库验证失败，已中止恢复: %w", err)
		}

		// 现库备份
		if _, err := os.Stat(dbPath); err == nil {
			bak := dbPath + ".bak-" + time.Now().Format("20060102150405")
			if err := copyFile(dbPath, bak); err != nil {
				return fmt.Errorf("备份当前数据库失败: %w", err)
			}
			fmt.Printf("当前数据库已备份到 %s\n", bak)
		}
		if err := os.WriteFile(dbPath, dbContent, 0o600); err != nil {
			return fmt.Errorf("写入数据库失败: %w", err)
		}
		// 清理残留 WAL/SHM（旧日志重放会导致新库损坏）
		for _, suffix := range []string{"-wal", "-shm"} {
			_ = os.Remove(dbPath + suffix)
		}
		fmt.Printf("恢复完成：%s\n请使用原 ARGUS_MASTER_KEY 与 ARGUS_JWT_SECRET 启动服务\n", dbPath)
		return nil
	},
}

func init() {
	backupCmd.Flags().StringP("output", "o", "", "备份文件路径（默认 argus-backup-<时间>.zip）")
	backupCmd.Flags().Bool("with-key", false, "将 ARGUS_MASTER_KEY 写入备份包（敏感，谨慎使用）")
	restoreCmd.Flags().StringP("input", "i", "", "备份 zip 文件路径")
	restoreCmd.Flags().Bool("force", false, "跳过确认提示")
	rootCmd.AddCommand(backupCmd)
	rootCmd.AddCommand(restoreCmd)
}
