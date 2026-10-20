package version

import "testing"

func TestCurrentStripsVPrefix(t *testing.T) {
	cases := []struct {
		name    string
		fctEnv  string
		appEnv  string
		wantSub string // 期望不出现 "vv"
	}{
		{"无环境变量", "", "", "1.0.7"},
		{"FCT_VERSION 带 v", "v1.0.7", "", "1.0.7"},
		{"FCT_VERSION 不带 v", "1.0.7", "", "1.0.7"},
		{"APP_VERSION 带 v", "", "v2.0.0", "2.0.0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("FCT_VERSION", c.fctEnv)
			t.Setenv("APP_VERSION", c.appEnv)
			got := Current()
			if got != c.wantSub {
				t.Fatalf("Current() = %q, want %q", got, c.wantSub)
			}
			if len(got) >= 2 && got[:2] == "vv" {
				t.Fatalf("Current() = %q 出现重复 v 前缀", got)
			}
		})
	}
}
