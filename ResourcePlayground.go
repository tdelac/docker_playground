package main

import (
	"bufio"
	"fmt"
	"github.com/fsouza/go-dockerclient"
	"gopkg.in/mgo.v2/bson"
	"os"
	"regexp"
	"strings"
	"time"
)

const (
	ENDPOINT   = "tcp://10.4.102.195:2376"
	USER       = "ubuntu"
	HOSTIP     = "10.4.102.195"
	IMG        = "ubuntu1404/builder"
	MINPORT    = 22000
	MAXPORT    = 22500
	SSHDPort   = "22/tcp"
	APITimeout = 5
)

var (
	client            *docker.Client
	spawnedContainers map[string]struct{}
	streamStatsDone   chan bool
)

func generateClient() error {
	var err error

	client, err = docker.NewTLSClient(ENDPOINT, "cert.pem", "key.pem", "ca.pem")
	if err != nil {
		return fmt.Errorf("Unable to create client: %v", err)
	}
	return nil
}

func populateHostConfig(hostConfig *docker.HostConfig, minPort, maxPort int64) error {
	// Get all the things!
	containers, err := client.ListContainers(docker.ListContainersOptions{})
	if err != nil {
		return fmt.Errorf("ListContainers API call failed: %v\n", err)
	}
	reservedPorts := make(map[int64]bool)
	for _, c := range containers {
		for _, p := range c.Ports {
			reservedPorts[p.PublicPort] = true
		}
	}

	// If unspecified, let Docker choose random port
	if minPort == 0 && maxPort == 0 {
		hostConfig.PublishAllPorts = true
		return nil
	}

	hostConfig.PortBindings = make(map[docker.Port][]docker.PortBinding)
	for i := minPort; i <= maxPort; i++ {
		// if port is not already in use, bind it to this exposed container port (k)
		if !reservedPorts[i] {
			hostConfig.PortBindings[SSHDPort] = []docker.PortBinding{
				docker.PortBinding{
					HostIP:   HOSTIP,
					HostPort: fmt.Sprintf("%v", i),
				},
			}
			break
		}
	}

	// If map is empty, no ports were available.
	if len(hostConfig.PortBindings) == 0 {
		return fmt.Errorf("No ports available")
	}
	return nil
}

func spawn(imageId string, minPort, maxPort int64) error {
	var err error

	if client == nil {
		return fmt.Errorf("Client must be initialized\n")
	}

	// Set ports and stuff
	hostConfig := &docker.HostConfig{}
	err = populateHostConfig(hostConfig, minPort, maxPort)

	// Create the container
	containerName := "docker-" + bson.NewObjectId().Hex()
	containerPtr, err := client.CreateContainer(
		docker.CreateContainerOptions{
			Name: containerName,
			Config: &docker.Config{
				Cmd: []string{"/usr/sbin/sshd", "-D"},
				ExposedPorts: map[docker.Port]struct{}{
					SSHDPort: struct{}{},
				},
				Image: imageId,
			},
			HostConfig: hostConfig,
		},
	)
	if err != nil {
		return fmt.Errorf("CreateContainer API call failed: %v")
	}
	fmt.Printf("Created container: %v\n", containerPtr.ID)

	// Kick off container
	err = client.StartContainer(containerPtr.ID, nil)
	if err != nil {
		// Clean up
		err2 := client.RemoveContainer(
			docker.RemoveContainerOptions{
				ID:    containerPtr.ID,
				Force: true,
			},
		)
		if err2 != nil {
			err = fmt.Errorf("%v. And was unable to clean up container %v: %v", err, containerPtr.ID, err2)
		}
		return fmt.Errorf("StartContainer API call failed: %v", err)
	}

	// On success, add container to queue of spawned containers
	spawnedContainers[containerPtr.ID] = struct{}{}

	return nil
}

func cleanUp() error {
	var err, err2 error

	// Attempt to reach client if connection not established
	if client == nil {
		err = generateClient()
		if err != nil {
			return fmt.Errorf("Containers may have been left un-reaped: %v", err)
		}
	}

	// Stop all running containers spawned by this program
	runningContainers, err := client.ListContainers(docker.ListContainersOptions{})
	if err != nil {
		return fmt.Errorf("Stop containers failed at ListContainers API call: %v", err)
	}
	for _, containerInfo := range runningContainers {
		if _, ok := spawnedContainers[containerInfo.ID]; ok {
			err2 = client.StopContainer(containerInfo.ID, APITimeout)
			if err2 != nil {
				err = fmt.Errorf("Unable to cleanly stop container %v: %v. ", containerInfo.ID, err2)
			}
		}
	}

	// Remove all containers spawned by this program (will force kill any that did not stop previously)
	existingContainers, err2 := client.ListContainers(
		docker.ListContainersOptions{
			All: true,
		},
	)
	if err2 != nil {
		return fmt.Errorf("Remove containers failed at ListContainers API call: %v. %v", err2, err)
	}
	for _, containerInfo := range existingContainers {
		if _, ok := spawnedContainers[containerInfo.ID]; ok {
			err2 = client.RemoveContainer(
				docker.RemoveContainerOptions{
					ID:    containerInfo.ID,
					Force: true,
				},
			)
			if err2 != nil {
				err = fmt.Errorf("Unable to remove container %v: %v. ", containerInfo.ID, err2)
			}
		}
	}

	if err != nil {
		return fmt.Errorf("Some containers may still exist: %v", err)
	}

	return nil
}

func streamStats(containerId string) error {
	var err error

	if client == nil {
		return fmt.Errorf("Client must be initialized\n")
	}

	// Initialize channels for communicating statistics
	stats := make(chan *docker.Stats)
	streamStatsDone = make(chan bool)

	// Goroutine to execute statistics stuff in background
	go func() {
		// Hmm guess we aren't responsible for closing?
		defer func() {
			//close(stats)
			//close(streamStatsDone)
		}()

		err = client.Stats(
			docker.StatsOptions{
				ID:      containerId,
				Stats:   stats,
				Stream:  true,
				Done:    streamStatsDone,
				Timeout: time.Second * 5,
			},
		)
		if err != nil {
			fmt.Printf("Unable to launch stats reporting: %v", err)
		}
	}()

	// Get all the datas
	go func() {
		// Awkwardly un-breakoutable
		for {
			retStats, ok := <-stats
			if ok {
				fmt.Printf("%v\n", retStats)
				continue
			}

			_, ok = <-streamStatsDone
			if !ok {
				break
			}
		}
	}()

	return nil
}

func parse(input string) {
	var err error

	input = strings.TrimSpace(strings.ToLower(input))
	switch input {
	case "help":
		fmt.Printf("Lol\n")
	case "make client", "generate client", "initialize client", "get client", "client", "gc":
		err = generateClient()
		if err != nil {
			fmt.Printf("%v\n", err)
			break
		}
		fmt.Printf("Client successfully created\n")
	case "spawn", "spawn container", "start container", "container up", "sc": // TODO unhardcode
		err = spawn(IMG, MINPORT, MAXPORT)
		if err != nil {
			fmt.Printf("%v\n", err)
			break
		}
		fmt.Printf("Container successfully spawned\n")
	case "stop stats", "end stats", "kill stats", "no stats":
		streamStatsDone <- true
		fmt.Printf("Killed stat streaming\n")
	case "exit", "quit", "bye", "gg":
		err = cleanUp()
		if err != nil {
			fmt.Printf("Error cleaning up resources\n")
			os.Exit(1)
		}
		fmt.Printf("Successfully cleaned up resources\n")
		fallthrough
	case "exit save":
		fmt.Printf("Goodbye!\n")
		os.Exit(0)
	}

	// Whatever
	if matched, _ := regexp.MatchString("get stats *", input); matched {
		cmdSlice := strings.Split(input, " ")
		if len(cmdSlice) < 3 {
			fmt.Printf("Invalid stats command\n")
			os.Exit(3)
		}
		streamStats(cmdSlice[2])
	}
}

func initialize() {
	spawnedContainers = make(map[string]struct{})
}

func main() {
	fmt.Printf("Welcome to the Docker Resource Allocator Playground. Type 'help' for options\n")

	initialize()
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Printf("$ ")
		input, err := reader.ReadString('\n')
		if err != nil {
			fmt.Printf("Improper read: %v\n", err)
			os.Exit(2)
		}
		parse(input)
	}
}
