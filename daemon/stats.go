package daemon

import (
	"encoding/json"
	"runtime"

	"golang.org/x/net/context"

	"github.com/Sirupsen/logrus"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/backend"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/container"
	"github.com/docker/docker/pkg/ioutils"
)

// ContainerStatsAll writes information about containers to the stream
// given in the config object.
func (daemon *Daemon) ContainerStatsAll(ctx context.Context, config *backend.ContainerStatsAllConfig) error {
	outStream := config.OutStream
	if config.Stream {
		wf := ioutils.NewWriteFlusher(outStream)
		defer wf.Close()
		wf.Flush()
		outStream = wf
	}

	enc := json.NewEncoder(outStream)

	updates := daemon.subscribeToContainerStatsAll()
	defer daemon.unsubscribeToContainerStatsAll(updates)

	for {
		select {
		case v, ok := <-updates:
			if !ok {
				return nil
			}

			statsJSON, ok := v.(map[string]*types.StatsJSON)
			if !ok {
				// malformed data!
				logrus.Errorf("receive malformed stats data")
				continue
			}

			var preCPUNotExits bool
			if !config.Stream {
				// prime the cpu stats so they aren't 0 in the final output
				for _, ss := range statsJSON {
					if ss.PreCPUStats == nil {
						preCPUNotExits = true
						break
					}
				}

				// if there's stats without preCPUNotExits
				if preCPUNotExits {
					continue
				}
			}

			// filter container
			filterdCtrs, err := daemon.reduceStatsContainers(config.Filters)
			if err != nil {
				logrus.Errorf("can't filter containers: %v", err)
				continue
			}

			survivedContainers := make(map[string]bool)
			for _, ctr := range filterdCtrs {
				survivedContainers[ctr.ID] = true
			}

			// if container didn't survive from the filter, remove it!
			var toDelete []string
			for id := range statsJSON {
				if _, ok := survivedContainers[id]; !ok {
					toDelete = append(toDelete, id)
				} else {
					delete(survivedContainers, id)
				}
			}

			// iterate the toDelete list, delete statsJSON with related id
			for _, id := range toDelete {
				delete(statsJSON, id)
			}

			// if we didn't get stats data for one survived container,
			// this means that the container isn't in running state,
			// we need to add an empty item for it in statsJSON
			for id := range survivedContainers {
				statsJSON[id] = &types.StatsJSON{}
			}
			if err := enc.Encode(statsJSON); err != nil {
				return err
			}

			if !config.Stream {
				return nil
			}
		case <-ctx.Done():
			return nil
		}
	}
}

func (daemon *Daemon) subscribeToContainerStatsAll() chan interface{} {
	return daemon.statsCollector.collectAll()
}

func (daemon *Daemon) unsubscribeToContainerStatsAll(ch chan interface{}) {
	daemon.statsCollector.unsubscribeAll(ch)
}

// GetContainerStats collects all the stats published by a container
func (daemon *Daemon) GetContainerStats(container *container.Container) (*types.StatsJSON, error) {
	stats, err := daemon.stats(container)
	if err != nil {
		return nil, err
	}

	// We already have the network stats on Windows directly from HCS.
	if !container.Config.NetworkDisabled && runtime.GOOS != "windows" {
		if stats.Networks, err = daemon.getNetworkStats(container); err != nil {
			return nil, err
		}
	}

	return stats, nil
}

// GetContainerStatsAllRunning collects stats of all the running containers
func (daemon *Daemon) GetContainerStatsAllRunning() map[string]*types.StatsJSON {
	allStats := make(map[string]*types.StatsJSON)
	containers := daemon.List()
	for _, cnt := range containers {
		if !cnt.IsRunning() {
			continue
		}
		stats, err := daemon.GetContainerStats(cnt)
		if err != nil {
			if _, ok := err.(errNotRunning); !ok {
				logrus.Errorf("collecting stats for %s: %v", cnt.ID, err)
			}
			continue
		}

		allStats[cnt.ID] = stats
	}
	return allStats
}

func (daemon *Daemon) reduceStatsContainers(filter filters.Args) ([]*types.Container, error) {
	config := &types.ContainerListOptions{
		All:     true,
		Size:    true,
		Filters: filter,
	}

	return daemon.reduceContainers(config, daemon.transformContainer)
}

// transformContainer generates the container type expected by the docker ps command.
func (daemon *Daemon) transformStatsContainer(container *container.Container, ctx *listContext) (*types.Container, error) {
	// For stats data, we only care about its ID for next step filtering
	newC := &types.Container{
		ID: container.ID,
	}
	return newC, nil
}
