// Package dockerfake contains logic for mocking out Docker-related
// functionality.
//
//go:generate mockgen -destination ./mock.go -package dockerfake github.com/coder/envbox/dockerutil DockerClient
package dockerfake
