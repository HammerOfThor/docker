package container

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/context"

	"github.com/docker/docker/api/client"
	"github.com/docker/docker/cli"
	"github.com/spf13/cobra"
)

type stopOptions struct {
	time int

	containers []string
}

// NewStopCommand creates a new cobra.Command for `docker stop`
func NewStopCommand(dockerCli *client.DockerCli) *cobra.Command {
	var opts stopOptions

	cmd := &cobra.Command{
		Use:   "stop [OPTIONS] CONTAINER [CONTAINER...]",
		Short: "Stop one or more running containers",
		Args:  cli.RequiresMinArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.containers = args
			return runStop(dockerCli, &opts)
		},
	}
	cmd.SetFlagErrorFunc(flagErrorFunc)

	flags := cmd.Flags()
	flags.IntVarP(&opts.time, "time", "t", 10, "Seconds to wait for stop before killing it")
	return cmd
}

func runStop(dockerCli *client.DockerCli, opts *stopOptions) error {
	ctx := context.Background()
	timeout := time.Duration(opts.time) * time.Second

	var waitAll sync.WaitGroup
	var printLock sync.Mutex
	var errLock sync.Mutex
	var errs []string

	var maxParallel int32 = 1000
	var count int32
	for _, container := range opts.containers {
		// wait if maxParallel number is reached
		atomic.AddInt32(&count, 1)
		for atomic.LoadInt32(&count) > maxParallel {
			time.Sleep(100 * time.Millisecond)
		}
		waitAll.Add(1)
		go func(toStop string) {
			defer waitAll.Done()
			defer atomic.AddInt32(&count, -1)
			if err := dockerCli.Client().ContainerStop(ctx, toStop, &timeout); err != nil {
				errLock.Lock()
				errs = append(errs, err.Error())
				errLock.Unlock()
			} else {
				printLock.Lock()
				fmt.Fprintf(dockerCli.Out(), "%s\n", toStop)
				printLock.Unlock()
			}
		}(container)
	}
	waitAll.Wait()
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "\n"))
	}
	return nil
}
