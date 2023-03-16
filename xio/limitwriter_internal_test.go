package xio

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrefixSuffixWriter(t *testing.T) {
	t.Parallel()

	type testcase struct {
		Name           string
		Input          string
		ExpectedOutput string
		N              int
	}

	testcases := []testcase{
		{
			Name:           "NoTruncate",
			Input:          "Test",
			ExpectedOutput: "Test",
			N:              2,
		},
		{
			Name:           "OutputTruncated",
			Input:          "Testing",
			ExpectedOutput: "Te" + skipMessage + "ng",
			N:              2,
		},
	}

	for _, test := range testcases {
		test := test
		t.Run(test.Name, func(t *testing.T) {
			t.Parallel()
			var (
				w   = bytes.Buffer{}
				psw = &PrefixSuffixWriter{
					W: &w,
					N: test.N,
				}
			)

			_, err := io.Copy(psw, strings.NewReader(test.Input))
			require.NoError(t, err, "copy")
			err = psw.Flush()
			require.NoError(t, err, "flush")
			require.Equal(t, test.ExpectedOutput, w.String(), "unexpected output")
		})
	}
}
