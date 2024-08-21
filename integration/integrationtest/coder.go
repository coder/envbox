package integrationtest

import (
	"testing"

	"github.com/coder/coder/v2/coderd/coderdtest"
	"github.com/coder/coder/v2/coderd/database"
	"github.com/coder/coder/v2/codersdk"
)

func NewCoderd(t *testing.T) (*codersdk.Client, database.Store) {
	t.Helper()

	client, db := coderdtest.NewWithDatabase(t, nil)
}
