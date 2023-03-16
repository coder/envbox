package integration

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	// Build the latest version of envbox to keep in sync with any developer
	// changes.
	buildEnvbox()

	os.Exit(m.Run())
}

func buildEnvbox() {
	dir, err := exec.Command("git", "rev-parse", "--show-toplevel").CombinedOutput()
	mustf(err, "output (%s)", string(dir))

	cmd := exec.Command("make", "-j", "build/image/envbox")
	cmd.Dir = strings.TrimSpace(string(dir))
	out, err := cmd.CombinedOutput()
	mustf(err, "make output (%s)", string(out))
}

func mustf(err error, msg string, args ...interface{}) {
	if err != nil {
		panic(fmt.Sprintf(msg+": %v", append(args, err)...))
	}
}
