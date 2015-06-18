package main

import (
	"fmt"
	"github.com/fsouza/go-dockerclient"
)

const (
	ENDPOINT = "unix:///var/run/docker.sock"
	TESTIMG  = "ubuntu"
)

var _ = fmt.Printf

func main() {
	client, err := docker.NewClient(ENDPOINT)
	if err != nil {
		fmt.Printf("Unable to create client - could be that ENDPOINT is invalid\n")
		return
	}

	containerPtr, err := client.CreateContainer(
		docker.CreateContainerOptions{
			Name: "trial",
			Config: &docker.Config{
				AttachStdin: true,
				Cmd:         []string{"sleep 100"},
				Image:       TESTIMG,
			},
			HostConfig: nil,
		},
	)
	if err != nil {
		fmt.Printf("Failed to create container id: %v. '%v'\n", TESTIMG, err)
		return
	}

	err = client.StartContainer(
		containerPtr.ID,
		nil,
	)
	if err != nil {
		fmt.Printf("Failed to start container %v. %v\n", containerPtr.ID, err)
	}
}
