package main

import (
	"./ui"
	"bufio"
	"encoding/csv"
	"fmt"
	"github.com/fsouza/go-dockerclient"
	"gopkg.in/mgo.v2/bson"
	"os"
	"regexp"
	"strings"
	"time"
)

const (
	ENDPOINT    = "tcp://10.4.102.195:2376"
	USER        = "ubuntu"
	HOSTIP      = "10.4.102.195"
	IMG         = "ubuntu1404/builder"
	MINPORT     = 22000
	MAXPORT     = 22500
	SSHDPort    = "22/tcp"
	APITimeout  = 5
	CSVDUMPFILE = "data.csv"
)

var (
	client            *docker.Client
	spawnedContainers map[string]struct{}
	streamStatsDone   chan bool

	// For data encoding. TODO streamline
	data [][]string
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

	// Stop running containers spawned by this program
	running, err2 := getContainers(docker.ListContainersOptions{})
	if err2 == nil {
		for _, id := range running {
			err2 = client.StopContainer(id, APITimeout)
			if err2 != nil {
				err = fmt.Errorf("%v Unable to cleanly stop container %v: %v.", err, id, err2)
			}
		}
	} else {
		err = fmt.Errorf("Unable to get any running containers")
	}

	// Remove existing containers spawned by this program
	existing, err2 := getContainers(docker.ListContainersOptions{All: true})
	if err2 == nil {
		for _, id := range existing {
			err2 = client.RemoveContainer(
				docker.RemoveContainerOptions{
					ID:    id,
					Force: true,
				},
			)
			if err2 != nil {
				err = fmt.Errorf("%v Unable to remove container %v: %v.", err, id, err2)
			}
		}
	} else {
		err = fmt.Errorf("Unable to get any existing containers. %v", err)
	}

	if err != nil {
		return fmt.Errorf("Some containers may still exist: %v", err)
	}

	return nil
}

func streamStats(containerIds []string) error {
	channelMap := make(map[string]chan *docker.Stats)
	dataMap := make(map[string]string)

	// To communicate with UI
	dataStream := make(chan map[string]string)
	ui.StreamData(dataStream)

	// Launch statistics reaping on each container
	for _, id := range containerIds {
		stats := make(chan *docker.Stats, 10) // buffer size is pretty arbitrary
		channelMap[id] = stats

		go func() {
			err = client.Stats(
				docker.StatsOptions{
					ID:     id,
					Stats:  stats,
					Stream: true,
					//Done: streamStatsDone,
					Timeout: time.Second * 5,
				},
			)
		}()
	}

	// Deal with returned stats
	go func() {
		for id, channel := range channelMap {
			select {
			case data, ok := <-channel:
				if ok {
					cacheStr := fmt.Sprintf("%v", data.MemoryStats.Stats.Cache)
					dataMap[id] = cacheStr
					fmt.Printf("Container %v returned %v\n", id, cacheStr)
					if len(dataMap) == len(containerIds) {
						dataMap["date"] = fmt.Sprintf("%v", time.Now().Unix())
						dataStream <- dataMap
						dataMap = make(map[string]string)
					}
				} else {
					dataMap[id] = "0"
				}
			}
		}
	}()
}

func streamStats(containerIds []string) error {
	var err error

	if client == nil {
		return fmt.Errorf("Client must be initialized\n")
	}

	// Initialize channels for communicating statistics
	streamStatsDone = make(chan bool)
	stats := make(chan *docker.Stats)

	// Set up data stream to ui
	dataStream := make(chan []string, 10)
	ui.StreamData(dataStream)

	// Start routine and communication channel for each container
	for _, id := range containerIds {
		stats = make(chan *docker.Stats)
		// Goroutine to execute statistics stuff in background
		go func() {
			err = client.Stats(
				docker.StatsOptions{
					ID:      id,
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
	}

	// Get all the datas
	go func() {
		// Awkwardly un-breakoutable
		for {
			retStats, ok := <-stats
			if ok {
				timeStr := fmt.Sprintf("%v", retStats.Read.Unix())
				cacheStr := fmt.Sprintf("%v", retStats.MemoryStats.Stats.Cache)
				data = append(data, []string{timeStr, cacheStr})
				dataStream <- []string{timeStr, cacheStr} // Send it off!
				fmt.Printf("%v\n", timeStr)
				fmt.Printf("%v\n", cacheStr)
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

func dumpCsv() error {
	// Create file
	file, err := os.Create(CSVDUMPFILE)
	if err != nil {
		return fmt.Errorf("Csv dump file creation failed: %v", err)
	}

	// Create csv writer and dump
	csvWriter := csv.NewWriter(file)
	err = csvWriter.WriteAll(data)
	if err != nil {
		return fmt.Errorf("Writing to csv dump file failed: %v", err)
	}

	return nil
}

func endDataCollection() {
	streamStatsDone <- true
	ui.ImportData(data)
}

func getContainers(opts docker.ListContainersOptions) ([]string, error) {
	if client == nil {
		generateClient()
	}

	allRunning, err := client.ListContainers(opts)
	if err != nil {
		return nil, fmt.Errorf("ListContainers API call failed: %v\n", err)
	}

	ret := make([]string, 0)
	for _, info := range allRunning {
		if _, ok := spawnedContainers[info.ID]; ok {
			ret = append(ret, info.ID)
		}
	}

	return ret, nil
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
		endDataCollection()
		fmt.Printf("Killed stat streaming\n")
	case "csv dump":
		err = dumpCsv()
		if err != nil {
			fmt.Printf("%v\n", err)
			break
		}
		fmt.Printf("Successfully dumped csv\n")
	case "all stats":
		running, err := getContainers(docker.ListContainersOptions{})
		if err != nil {
			fmt.Printf("%v", err)
			break
		}
		streamStats(running)
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
		streamStats([]string{cmdSlice[2]})
	}
}

func initialize() {
	spawnedContainers = make(map[string]struct{})
	data = make([][]string, 0)
	data = append(data, []string{"date", "cache"})
}

func main() {
	fmt.Printf("Welcome to the Docker Resource Allocator Playground. Type 'help' for options\n")

	initialize()
	ui.StartUIServer()
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
