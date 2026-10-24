package protonmail

import (
	"errors"
	"fmt"
	"testing"

	"github.com/ProtonMail/go-proton-api"
)

func TestIsAuthLost(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"offline", &proton.NetError{Message: "down", Cause: errors.New("dial")}, false},
		{"refresh invalid", &proton.APIError{Status: 422, Code: proton.AuthRefreshTokenInvalid, Message: "bad"}, true},
		{"password wrong", &proton.APIError{Status: 401, Code: proton.PasswordWrong, Message: "no"}, true},
		{"http 400", &proton.APIError{Status: 400, Message: "bad request"}, true},
		{"http 500", &proton.APIError{Status: 500, Message: "oops"}, false},
		{"http 503", &proton.APIError{Status: 503, Message: "busy"}, false},
		{"de-auth wrap", fmt.Errorf("failed to refresh auth, de-auth: %w", errors.New("no")), true},
		{"generic", errors.New("something else"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsAuthLost(tc.err); got != tc.want {
				t.Fatalf("IsAuthLost(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
