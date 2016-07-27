package container

import (
	"fmt"
	"strings"
	"sync"
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
	//A buffered channel can be used like a semaphore, for instance to limit throughput. https://golang.org/doc/effective_go.html#channels
	var sem = make(chan int, maxParallel)
	for _, container := range opts.containers {
		sem <- 1 // Wait for active queue sem to drain.
		waitAll.Add(1)
		go func(toStop string) {
			defer waitAll.Done()

			if err := dockerCli.Client().ContainerStop(ctx, toStop, &timeout); err != nil {
				errLock.Lock()
				errs = append(errs, err.Error())
				errLock.Unlock()
			} else {
				printLock.Lock()
				fmt.Fprintf(dockerCli.Out(), "%s\n", toStop)
				printLock.Unlock()
			}
			<-sem //Done, enable next
		}(container)
	}
	waitAll.Wait()
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "\n"))
	}
	return nil
}
