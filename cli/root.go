package cli

import (
	"github.com/spf13/cobra"
)

type lifecycle struct {
	armed bool
	done  chan struct{}
}

func newLifecycle() *lifecycle {
	return &lifecycle{done: make(chan struct{})}
}

func (l *lifecycle) arm() {
	if l != nil {
		l.armed = true
	}
}

func (l *lifecycle) finish() {
	if l != nil {
		close(l.done)
	}
}

func (l *lifecycle) wait() {
	if l != nil && l.armed {
		<-l.done
	}
}

// Execute runs envbox and keeps the process alive until an active workload has
// completed its shutdown sequence.
func Execute() error {
	lifecycle := newLifecycle()
	_, err := root(lifecycle).ExecuteC()
	if err != nil {
		return err
	}
	lifecycle.wait()
	return nil
}

func Root() *cobra.Command {
	return root(nil)
}

func root(lifecycle *lifecycle) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "envbox",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(dockerCmd(lifecycle))
	return cmd
}
