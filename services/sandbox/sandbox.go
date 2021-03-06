package sandbox

import (
	"archive/tar"
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"github.com/docker/engine-api/client"
	"github.com/docker/engine-api/types"
	"github.com/docker/engine-api/types/container"
	"github.com/docker/engine-api/types/network"
	"github.com/docker/go-connections/nat"
	"golang.org/x/net/context"
	"io"
	"io/ioutil"
	"log"
	"net"
	"strconv"
	"strings"
	"time"
)

const ServerDropDir = "/botbox-server"
const ClientDropDir = "/botbox-client"
const StateLogFile = "state.log"
const ResultLogFile = "result.log"
const ConnectLogFile = "connect.log"
const DisconnectLogFile = "disconnect.log"

const ServerUser = "sandbox"
const ClientUser = "sandbox"
const ServerImageName = "botbox-sandbox-server"
const ClientImageName = "botbox-sandbox-client"
const ClientServerEnvVar = "BOTBOX_SERVER"
const ClientSecretEnvVar = "BOTBOX_SECRET"
const ServerIdsEnvVar = "BOTBOX_IDS"
const ServerSecretEnvVar = "BOTBOX_SECRETS"
const SecretLength = 64
const EnvListSep = " "

// Convert a directory into a tar file to pass to the Docker image build
// Path should end with a trailing slash
func tarFile(path string) (io.Reader, error) {
	dir, err := ioutil.ReadDir(path)
	if err != nil {
		return nil, err
	}

	buf := bytes.NewBuffer(nil)
	tr := tar.NewWriter(buf)
	for _, f := range dir {
		contents, err := ioutil.ReadFile(path + f.Name())
		if err != nil {
			return nil, err
		}
		tr.WriteHeader(&tar.Header{
			Name: f.Name(),
			Size: int64(f.Size()),
		})
		tr.Write(contents)
	}
	tr.Close()
	return bytes.NewReader(buf.Bytes()), nil
}

// Generate a list of n cryptographically secure secrets.
func GenerateSecrets(n int) ([]string, error) {
	output := make([]string, n)
	for i := 0; i < n; i++ {
		b := make([]byte, SecretLength)
		_, err := rand.Read(b)
		if err != nil {
			return nil, err
		}
		output[i] = base64.RawURLEncoding.EncodeToString(b)
	}
	return output, nil
}

// Build a docker image from a Dockerfile with the given name
func BuildImage(cli *client.Client, path, name string) ([]byte, error) {
	file, err := tarFile(path)
	if err != nil {
		return nil, err
	}
	reader := bufio.NewReader(file)
	opts := types.ImageBuildOptions{Tags: []string{name}}
	response, err := cli.ImageBuild(context.Background(), reader, opts)
	if err != nil {
		return nil, err
	}
	buf := new(bytes.Buffer)
	buf.ReadFrom(response.Body)
	response.Body.Close()
	return buf.Bytes(), nil
}

// Blocks until the server container stops.
func Wait(cli *client.Client, serverId string) error {
	log.Println("Waiting for container to stop.")
	_, err := cli.ContainerWait(context.Background(), serverId)
	if err != nil {
		return err
	}
	log.Println("Container stopped.")

	return nil
}

// Get container STDIN/STDOUT results
func ContainerLogs(cli *client.Client, id string) ([]byte, error) {
	opts := types.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
	}
	rc, err := cli.ContainerLogs(context.Background(), id, opts)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	o, err := ioutil.ReadAll(rc)
	if err != nil {
		return nil, err
	}
	return o, nil
}

// Get a file from a container and return its contents as a byte array.
func getFile(
	cli *client.Client, containerId string, path string,
) ([]byte, error) {
	rc, _, err := cli.CopyFromContainer(context.Background(), containerId, path)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	contents, err := ioutil.ReadAll(rc)
	if err != nil {
		return nil, err
	}
	b := bytes.NewReader(contents)
	archive, err := OpenTar(b)
	if err != nil {
		return nil, err
	}
	files, err := archive.Files()
	if err != nil {
		return nil, err
	}

	contents, err = ioutil.ReadAll(files[0].Reader)
	if err != nil {
		return nil, err
	}

	return contents, nil
}

// Get the list of clients who successfully connected from the connect.log
// file inside the container.
func ClientsConnected(cli *client.Client, serverId string) ([]string, error) {
	path := ServerDropDir + "/" + ConnectLogFile
	contents, err := getFile(cli, serverId, path)
	if err != nil {
		return nil, err
	}

	ids, err := bytes.Split(bytes.TrimSpace(contents), []byte("\n")), nil
	if err != nil {
		return nil, err
	}
	output := make([]string, len(ids))

	for i, id := range ids {
		output[i] = string(id)
	}

	return output, nil
}

// Get the list of clients who committed a sin from the disconnect.log
// file inside the container.
func BadClients(cli *client.Client, serverId string) ([]string, error) {
	path := ServerDropDir + "/" + DisconnectLogFile
	contents, err := getFile(cli, serverId, path)
	if err != nil {
		return nil, err
	}

	ids, err := bytes.Split(bytes.TrimSpace(contents), []byte("\n")), nil
	if err != nil {
		return nil, err
	}

	output := make([]string, 0)

	for _, id := range ids {
		if string(id) != "" {
			output = append(output, string(id))
		}
	}

	return output, nil
}

// Get the results for each client from the server.
func GameResult(cli *client.Client, serverId string) ([]int, error) {
	path := ServerDropDir + "/" + ResultLogFile
	contents, err := getFile(cli, serverId, path)
	if err != nil {
		return nil, err
	}

	parsed := make([]float64, 0)
	err = json.Unmarshal(contents, &parsed)
	if err != nil {
		return nil, err
	}

	result := make([]int, len(parsed))
	for i, v := range parsed {
		result[i] = int(v)
	}

	return result, nil
}

// Get a list of state histories from the state.log file
func GameHistory(cli *client.Client, serverId string) ([]interface{}, error) {
	path := ServerDropDir + "/" + StateLogFile
	contents, err := getFile(cli, serverId, path)
	if err != nil {
		return nil, err
	}

	states, err := bytes.Split(bytes.TrimSpace(contents), []byte("\n")), nil
	if err != nil {
		return nil, err
	}
	output := make([]interface{}, len(states))

	for i, v := range states {
		output[i] = json.Unmarshal(v, &output[i])
	}

	return output, nil
}

// Destroy a sandbox by passing it a list of container ids and the network id.
// It will disconnect clients from the network, remove the containers, and
// then remove the network.
func DestroySandbox(cli *client.Client, network string, containers []string) error {
	log.Println("Destroying sandbox.")
	removeOpts := types.ContainerRemoveOptions{Force: true}
	for _, c := range containers {
		err := cli.NetworkDisconnect(context.Background(), network, c, true)
		if err != nil {
			return err
		}
		err = cli.ContainerRemove(context.Background(), c, removeOpts)
		if err != nil {
			return err
		}
	}

	cli.NetworkRemove(context.Background(), network)
	return nil
}

// Setup a Docker bridge network to connect the server with the clients.
func SetupNetwork(cli *client.Client) (string, error) {
	t := time.Now().Unix()
	name := "sandbox_" + strconv.FormatInt(t, 10)
	createConfig := types.NetworkCreate{
		Driver:   "bridge",
		Internal: true,
	}
	netResponse, err := cli.NetworkCreate(
		context.Background(),
		name,
		createConfig,
	)

	if err != nil {
		return "", err
	}

	return netResponse.ID, nil
}

// Setup a server sandbox in an isolated container. Returns the ID of the
// container if it was created successfully.
func SetupServer(
	cli *client.Client,
	ids, secrets []string,
	archive Archive,
) (string, error) {

	// create container, but don't start it
	containerConfig := &container.Config{
		Cmd:          []string{"/bin/bash", "run.sh"},
		WorkingDir:   ServerDropDir,
		User:         ServerUser,
		Image:        ServerImageName,
		ExposedPorts: map[nat.Port]struct{}{nat.Port("12345/tcp"): struct{}{}},
		Env: []string{
			ServerIdsEnvVar + "=" + strings.Join(ids, EnvListSep),
			ServerSecretEnvVar + "=" + strings.Join(secrets, EnvListSep),
		},
	}
	// TODO: send score results to scoreboard service
	hostConfig := &container.HostConfig{}
	netConfig := &network.NetworkingConfig{}
	log.Println("Creating server container.")
	response, err := cli.ContainerCreate(
		context.Background(),
		containerConfig,
		hostConfig,
		netConfig,
		"",
	)

	if err != nil {
		return "", err
	}

	log.Println("Copying server files.")
	tar, err := ArchiveToTar(archive)
	if err != nil {
		return "", err
	}
	err = cli.CopyToContainer(
		context.Background(),
		response.ID,
		ServerDropDir,
		tar,
		types.CopyToContainerOptions{},
	)

	return response.ID, nil
}

// Start the server container and connect it to the network. Return the IP
// assigned to the server on the network or an error.
func StartServer(cli *client.Client, netId, servId string) (string, error) {
	// Connect server to the network.
	log.Println("Connecting server.")
	servEpSet := &network.EndpointSettings{}

	err := cli.NetworkConnect(context.Background(), netId, servId, servEpSet)
	if err != nil {
		return "", err
	}

	log.Println("Starting server")
	startOpts := types.ContainerStartOptions{}
	err = cli.ContainerStart(context.Background(), servId, startOpts)
	if err != nil {
		return "", err
	}

	// Get the server IP address on the network
	netInfo, err := cli.NetworkInspect(context.Background(), netId)
	if err != nil {
		return "", err
	}
	servIp, _, err := net.ParseCIDR(netInfo.Containers[servId].IPv4Address)
	if err != nil {
		return "", err
	}

	log.Println("Server IP: " + servIp.String())

	return servIp.String(), nil
}

// Setup a client sandbox in an isolated container. Returns the ID of the
// container if it was created successfully.
func SetupClient(cli *client.Client, netId, serverIP, secret string, archive Archive) (string, error) {

	// create container, but don't start it
	containerConfig := &container.Config{
		Cmd:        []string{"/bin/bash", "run.sh"},
		WorkingDir: ClientDropDir,
		User:       ClientUser,
		Image:      ClientImageName,
		Env: []string{
			ClientServerEnvVar + "=" + serverIP,
			ClientSecretEnvVar + "=" + secret,
		},
	}
	hostConfig := &container.HostConfig{
		NetworkMode: container.NetworkMode(netId),
		CapDrop:     []string{"all"},
	}
	netConfig := &network.NetworkingConfig{}
	log.Println("Creating client container.")
	response, err := cli.ContainerCreate(
		context.Background(),
		containerConfig,
		hostConfig,
		netConfig,
		"",
	)

	if err != nil {
		return "", err
	}

	log.Println("Copying client files to container.")
	tar, err := ArchiveToTar(archive)
	if err != nil {
		return "", nil
	}
	err = cli.CopyToContainer(
		context.Background(),
		response.ID,
		ClientDropDir,
		tar,
		types.CopyToContainerOptions{},
	)

	return response.ID, nil
}

// Start a client container and connect it to the network.
func StartClient(cli *client.Client, netId, clientId string) error {
	// Connect server to the network.

	log.Println("Starting client")
	startOpts := types.ContainerStartOptions{}
	err := cli.ContainerStart(context.Background(), clientId, startOpts)
	if err != nil {
		return err
	}

	return nil
}

func SetupClients(cli *client.Client, netId, serverIp string, secrets []string, archives []Archive) ([]string, error) {
	clientIds := []string{}
	for i, a := range archives {
		clientId, err := SetupClient(cli, netId, serverIp, secrets[i], a)
		if err != nil {
			return nil, err
		}
		clientIds = append(clientIds, clientId)
	}

	return clientIds, nil
}

func StartClients(cli *client.Client, netId string, clientIds []string) error {
	for _, clientId := range clientIds {
		err := StartClient(cli, netId, clientId)
		if err != nil {
			return err
		}
	}
	return nil
}
