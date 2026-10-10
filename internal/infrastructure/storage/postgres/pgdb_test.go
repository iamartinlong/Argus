package postgres

import "testing"

func TestRewriteQuery(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"SELECT * FROM users WHERE id = ?", "SELECT * FROM users WHERE id = $1"},
		{"SELECT * FROM users WHERE id = ? AND name = ?", "SELECT * FROM users WHERE id = $1 AND name = $2"},
		{"INSERT INTO t (a, b) VALUES (?, ?)", "INSERT INTO t (a, b) VALUES ($1, $2)"},
		// 无参数
		{"SELECT COUNT(1) FROM t", "SELECT COUNT(1) FROM t"},
		// LIKE ESCAPE 保持不变
		{`SELECT 1 WHERE a LIKE ? ESCAPE '\'`, `SELECT 1 WHERE a LIKE $1 ESCAPE '\'`},
	}
	for _, c := range cases {
		if got := rewriteQuery(c.in); got != c.want {
			t.Fatalf("rewriteQuery(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRewriteInsertOrIgnore(t *testing.T) {
	in := `INSERT OR IGNORE INTO notification_logs (id, cert_id, rule_id, days_before, sent_at) VALUES (?, ?, ?, ?, ?)`
	want := `INSERT INTO notification_logs (id, cert_id, rule_id, days_before, sent_at) VALUES ($1, $2, $3, $4, $5) ON CONFLICT DO NOTHING`
	if got := rewriteQuery(in); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestRewriteInsertOrIgnoreNestedValues 嵌套括号（如函数调用）也能正确闭合
func TestRewriteInsertOrIgnoreNestedValues(t *testing.T) {
	in := `INSERT OR IGNORE INTO t (a) VALUES (coalesce(?, 0))`
	want := `INSERT INTO t (a) VALUES (coalesce($1, 0)) ON CONFLICT DO NOTHING`
	if got := rewriteQuery(in); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
