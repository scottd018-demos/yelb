//go:build unit

package function

import (
	"testing"

	_ "github.com/lib/pq"
)

func Test_normalizeApiPath(t *testing.T) {
	type args struct {
		apiPath string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "ensure normal path remains normal",
			args: args{
				apiPath: "/api/test",
			},
			want: "/api/test",
		},
		{
			name: "ensure abnormal path returns normal",
			args: args{
				apiPath: "//////api/test",
			},
			want: "/api/test",
		},
		{
			name: "ensure abnormal path without spaces returns normal",
			args: args{
				apiPath: "api/test",
			},
			want: "/api/test",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeApiPath(tt.args.apiPath); got != tt.want {
				t.Errorf("normalizeApiPath() = %v, want %v", got, tt.want)
			}
		})
	}
}
