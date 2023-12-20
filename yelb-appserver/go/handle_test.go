package function

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "github.com/lib/pq"
)

// TestHandle ensures that Handle executes without error and returns the
// HTTP 200 status code indicating no errors.
func TestHandle(t *testing.T) {
	var (
		w   = httptest.NewRecorder()
		req = httptest.NewRequest("GET", "http://example.com/test", nil)
		res *http.Response
	)

	Handle(context.Background(), w, req)
	res = w.Result()
	defer res.Body.Close()

	if res.StatusCode != 200 {
		t.Fatalf("unexpected response code: %v", res.StatusCode)
	}
}

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
