package v2

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/porter-dev/porter/api/types"
	"github.com/porter-dev/porter/cli/cmd/pack"

	"github.com/porter-dev/porter/cli/cmd/docker"

	api "github.com/porter-dev/porter/api/client"
)

const (
	buildMethodPack   = "pack"
	buildMethodDocker = "docker"
)

// buildInput is the input struct for the build method
type buildInput struct {
	ProjectID uint
	// AppName is the name of the application being built and is used to name the repository
	AppName      string
	BuildContext string
	Dockerfile   string
	BuildMethod  string
	// Builder is the image containing the components necessary to build the application in a pack build
	Builder    string
	BuildPacks []string
	// ImageTag is the tag to apply to the new image
	ImageTag string
	// CurrentImageTag is used in docker build to cache from
	CurrentImageTag string
	RepositoryURL   string
}

// build will create an image repository if it does not exist, and then build and push the image
func build(ctx context.Context, client api.Client, inp buildInput) error {
	if inp.ProjectID == 0 {
		return errors.New("must specify a project id")
	}
	projectID := inp.ProjectID

	if inp.ImageTag == "" {
		return errors.New("must specify an image tag")
	}
	tag := inp.ImageTag

	if inp.RepositoryURL == "" {
		return errors.New("must specify a registry url")
	}
	imageURL := strings.TrimPrefix(inp.RepositoryURL, "https://")

	err := createImageRepositoryIfNotExists(ctx, client, projectID, imageURL)
	if err != nil {
		return fmt.Errorf("error creating image repository: %w", err)
	}

	dockerAgent, err := docker.NewAgentWithAuthGetter(ctx, client, projectID)
	if err != nil {
		return fmt.Errorf("error getting docker agent: %w", err)
	}

	switch inp.BuildMethod {
	case buildMethodDocker:
		basePath, err := filepath.Abs(".")
		if err != nil {
			return fmt.Errorf("error getting absolute path: %w", err)
		}

		buildCtx, dockerfilePath, isDockerfileInCtx, err := resolveDockerPaths(
			basePath,
			inp.BuildContext,
			inp.Dockerfile,
		)
		if err != nil {
			return fmt.Errorf("error resolving docker paths: %w", err)
		}

		opts := &docker.BuildOpts{
			ImageRepo:         inp.RepositoryURL,
			Tag:               tag,
			CurrentTag:        inp.CurrentImageTag,
			BuildContext:      buildCtx,
			DockerfilePath:    dockerfilePath,
			IsDockerfileInCtx: isDockerfileInCtx,
		}

		err = dockerAgent.BuildLocal(
			ctx,
			opts,
		)
		if err != nil {
			return fmt.Errorf("error building image with docker: %w", err)
		}
	case buildMethodPack:
		packAgent := &pack.Agent{}

		opts := &docker.BuildOpts{
			ImageRepo:    imageURL,
			Tag:          tag,
			BuildContext: inp.BuildContext,
		}

		buildConfig := &types.BuildConfig{
			Builder:    inp.Builder,
			Buildpacks: inp.BuildPacks,
		}

		err := packAgent.Build(ctx, opts, buildConfig, "")
		if err != nil {
			return fmt.Errorf("error building image with pack: %w", err)
		}
	default:
		return fmt.Errorf("invalid build method: %s", inp.BuildMethod)
	}

	err = dockerAgent.PushImage(ctx, fmt.Sprintf("%s:%s", imageURL, tag))
	if err != nil {
		return fmt.Errorf("error pushing image url: %w\n", err)
	}

	return nil
}

func createImageRepositoryIfNotExists(ctx context.Context, client api.Client, projectID uint, imageURL string) error {
	if projectID == 0 {
		return errors.New("must specify a project id")
	}

	if imageURL == "" {
		return errors.New("must specify an image url")
	}

	regList, err := client.ListRegistries(ctx, projectID)
	if err != nil {
		return fmt.Errorf("error calling list registries: %w", err)
	}

	if regList == nil {
		return errors.New("registry list is nil")
	}

	if len(*regList) == 0 {
		return errors.New("no registries found for project")
	}

	var registryID uint
	for _, registry := range *regList {
		if strings.Contains(strings.TrimPrefix(imageURL, "https://"), strings.TrimPrefix(registry.URL, "https://")) {
			registryID = registry.ID
			break
		}
	}

	if registryID == 0 {
		return errors.New("no registries match url")
	}

	err = client.CreateRepository(
		ctx,
		projectID,
		registryID,
		&types.CreateRegistryRepositoryRequest{
			ImageRepoURI: imageURL,
		},
	)
	if err != nil {
		return fmt.Errorf("error creating repository: %w", err)
	}

	return nil
}

// resolveDockerPaths returns a path to the dockerfile that is either relative or absolute, and a path
// to the build context that is absolute.
//
// The return value will be relative if the dockerfile exists within the build context, absolute
// otherwise. The second return value is true if the dockerfile exists within the build context,
// false otherwise.
func resolveDockerPaths(basePath string, buildContextPath string, dockerfilePath string) (
	absoluteBuildContextPath string,
	outputDockerfilePath string,
	isDockerfileRelative bool,
	err error,
) {
	absoluteBuildContextPath, err = filepath.Abs(buildContextPath)
	if err != nil {
		return "", "", false, fmt.Errorf("error getting absolute path: %w", err)
	}
	outputDockerfilePath = dockerfilePath

	if !filepath.IsAbs(dockerfilePath) {
		outputDockerfilePath = filepath.Join(basePath, dockerfilePath)
	}

	pathComp, err := filepath.Rel(absoluteBuildContextPath, outputDockerfilePath)
	if err != nil {
		return "", "", false, fmt.Errorf("error getting relative path: %w", err)
	}

	if !strings.HasPrefix(pathComp, ".."+string(os.PathSeparator)) {
		isDockerfileRelative = true
		return absoluteBuildContextPath, pathComp, isDockerfileRelative, nil
	}
	isDockerfileRelative = false

	outputDockerfilePath, err = filepath.Abs(outputDockerfilePath)
	if err != nil {
		return "", "", false, err
	}

	return absoluteBuildContextPath, outputDockerfilePath, isDockerfileRelative, nil
}
