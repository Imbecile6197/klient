package macbridge

import (
	"fmt"

	"github.com/zalando/go-keyring"
)

const smokeService = "klient"
const smokeUser = "macos-bridge-smoke"

// keyringSmoke writes and reads a throwaway keyring item (not a real session).
func keyringSmoke() Response {
	const want = "ok"
	if err := keyring.Set(smokeService, smokeUser, want); err != nil {
		return Response{OK: false, Error: fmt.Sprintf("set: %v", err)}
	}
	got, err := keyring.Get(smokeService, smokeUser)
	if err != nil {
		return Response{OK: false, Error: fmt.Sprintf("get: %v", err)}
	}
	_ = keyring.Delete(smokeService, smokeUser)
	if got != want {
		return Response{OK: false, Error: "value mismatch"}
	}
	return Response{OK: true, Result: map[string]string{"status": "keyring ok"}}
}
