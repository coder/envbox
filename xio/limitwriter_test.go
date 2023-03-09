package xio

import (
	"bytes"
	cryptorand "crypto/rand"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLimitWriter(t *testing.T) {
	type writeCase struct {
		N    int
		ExpN int
		Err  bool
	}

	// testCases will do multiple writes to the same limit writer and check the output.
	testCases := []struct {
		Name   string
		L      int64
		Writes []writeCase
		N      int
		ExpN   int
	}{
		{
			Name: "Empty",
			L:    1000,
			Writes: []writeCase{
				// A few empty writes
				{N: 0, ExpN: 0}, {N: 0, ExpN: 0}, {N: 0, ExpN: 0},
			},
		},
		{
			Name: "NotFull",
			L:    1000,
			Writes: []writeCase{
				{N: 250, ExpN: 250},
				{N: 250, ExpN: 250},
				{N: 250, ExpN: 250},
			},
		},
		{
			Name: "Short",
			L:    1000,
			Writes: []writeCase{
				{N: 250, ExpN: 250},
				{N: 250, ExpN: 250},
				{N: 250, ExpN: 250},
				{N: 250, ExpN: 250},
				{N: 250, ExpN: 0, Err: true},
			},
		},
		{
			Name: "Exact",
			L:    1000,
			Writes: []writeCase{
				{
					N:    1000,
					ExpN: 1000,
				},
				{
					N:   1000,
					Err: true,
				},
			},
		},
		{
			Name: "Over",
			L:    1000,
			Writes: []writeCase{
				{
					N:    5000,
					ExpN: 1000,
					Err:  false,
				},
				{
					N:   5000,
					Err: true,
				},
				{
					N:   5000,
					Err: true,
				},
			},
		},
		{
			Name: "Strange",
			L:    -1,
			Writes: []writeCase{
				{
					N:    5,
					ExpN: 0,
					Err:  true,
				},
				{
					N:    0,
					ExpN: 0,
					Err:  true,
				},
			},
		},
	}

	for _, c := range testCases {
		t.Run(c.Name, func(t *testing.T) {
			buf := bytes.NewBuffer([]byte{})
			allBuff := bytes.NewBuffer([]byte{})
			w := NewLimitWriter(buf, c.L)

			for _, wc := range c.Writes {
				data := make([]byte, wc.N)

				n, err := cryptorand.Read(data)
				require.NoError(t, err, "crand read")
				require.Equal(t, wc.N, n, "correct bytes read")
				max := data[:wc.ExpN]
				n, err = w.Write(data)
				if wc.Err {
					require.Error(t, err, "exp error")
				} else {
					require.NoError(t, err, "write")
				}

				// Need to use this to compare across multiple writes.
				// Each write appends to the expected output.
				allBuff.Write(max)

				require.Equal(t, wc.ExpN, n, "correct bytes written")
				require.Equal(t, allBuff.Bytes(), buf.Bytes(), "expected data")
			}
		})
	}
}

func TestPrefixSuffixWriter(t *testing.T) {
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
		t.Run(test.Name, func(t *testing.T) {
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
