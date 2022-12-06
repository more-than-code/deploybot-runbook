package util

import (
	"context"
	"io"
	"log"
	"os"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
	"github.com/kelseyhightower/envconfig"
	"github.com/more-than-code/deploybot/model"
)

type ContainerHelperConfig struct {
	RegistryAuth string `envconfig:"REGISTRY_AUTH"`
}

type ContainerHelper struct {
	cli *client.Client
	cfg ContainerHelperConfig
}

func NewContainerHelper(host string) *ContainerHelper {
	var cfg ContainerHelperConfig
	err := envconfig.Process("", &cfg)
	if err != nil {
		panic(err)
	}

	cli, err := client.NewClientWithOpts(client.WithHost(host), client.WithAPIVersionNegotiation())
	if err != nil {
		panic(err)
	}
	return &ContainerHelper{cli: cli, cfg: cfg}
}

func (h *ContainerHelper) BuildImage(buildContext io.Reader, buidOptions *types.ImageBuildOptions) error {
	buildResponse, err := h.cli.ImageBuild(context.Background(), buildContext, *buidOptions)

	if err != nil {
		return err
	}

	res, err := io.ReadAll(buildResponse.Body)
	if err != nil {
		return err
	}

	log.Println(string(res))

	buildResponse.Body.Close()

	return nil
}

func (h *ContainerHelper) PushImage(imageTag string) error {
	res, err := h.cli.ImagePush(context.Background(), imageTag, types.ImagePushOptions{RegistryAuth: h.cfg.RegistryAuth})

	if err != nil {
		return err
	}

	res.Close()

	return nil
}

func (h *ContainerHelper) StartContainer(cfg *model.DeployConfig) {
	ctx := context.Background()

	h.cli.ContainerStop(ctx, cfg.ServiceName, nil)
	h.cli.ContainerRemove(ctx, cfg.ServiceName, types.ContainerRemoveOptions{})

	reader, err := h.cli.ImagePull(ctx, cfg.ImageName, types.ImagePullOptions{})
	if err != nil {
		panic(err)
	}
	io.Copy(os.Stdout, reader)

	resp, err := h.cli.ContainerCreate(ctx, &container.Config{
		Image:        cfg.ImageName,
		Env:          cfg.Env,
		ExposedPorts: nat.PortSet{nat.Port(cfg.ExposedPort + "/tcp"): struct{}{}},
	}, &container.HostConfig{
		AutoRemove:   cfg.AutoRemove,
		Mounts:       cfg.Mounts,
		PortBindings: nat.PortMap{nat.Port(cfg.HostPort + "/tcp"): []nat.PortBinding{{HostPort: cfg.HostPort, HostIP: "0.0.0.0"}}},
	}, &network.NetworkingConfig{}, nil, cfg.ServiceName)
	if err != nil {
		panic(err)
	}

	if err := h.cli.ContainerStart(ctx, resp.ID, types.ContainerStartOptions{}); err != nil {
		panic(err)
	}

	statusCh, errCh := h.cli.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)

	select {
	case err := <-errCh:
		if err != nil {
			panic(err)
		}
	case <-statusCh:
	}

	out, err := h.cli.ContainerLogs(ctx, resp.ID, types.ContainerLogsOptions{ShowStdout: true})
	if err != nil {
		panic(err)
	}

	stdcopy.StdCopy(os.Stdout, os.Stderr, out)
}
