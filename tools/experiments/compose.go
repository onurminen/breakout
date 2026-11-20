package main

import (
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

const composeFile = "deploy/docker-compose.yml"

// compose runs docker compose with extra environment variables, which the
// Compose file reads for the ingestion and engine settings.
func compose(env map[string]string, args ...string) {
	command := exec.Command("docker", append([]string{"compose", "-f", composeFile, "--profile", "pipeline"}, args...)...)
	command.Env = os.Environ()
	for name, value := range env {
		command.Env = append(command.Env, name+"="+value)
	}
	command.Stdout, command.Stderr = os.Stderr, os.Stderr
	if err := command.Run(); err != nil {
		fail("docker compose %s: %v", strings.Join(args, " "), err)
	}
}

// cleanStart removes all containers, which deletes Kafka's and Postgres's data,
// and starts the infrastructure. The pipeline services depend on kafka-init, so
// the first of them to start creates the topics.
func cleanStart() {
	log.Print("clean start")
	compose(nil, "down")
	compose(nil, "up", "-d", "--wait", "kafka", "postgres", "prometheus", "grafana", "cadvisor", "kafka-exporter")
}

// waitForIngestion waits until ingestion has sent every file, then stops it.
func waitForIngestion(timeout time.Duration) {
	waitFor("ingestion done", timeout, func() bool {
		return strings.Contains(composeOutput("logs", "ingestion"), "done;")
	})
	compose(nil, "stop", "ingestion")
}

func composeOutput(args ...string) string {
	output, err := exec.Command("docker", append([]string{"compose", "-f", composeFile, "--profile", "pipeline"}, args...)...).CombinedOutput()
	if err != nil {
		fail("docker compose %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return string(output)
}

// waitFor polls done every second until it returns true, or fails after timeout.
func waitFor(what string, timeout time.Duration, done func() bool) {
	deadline := time.Now().Add(timeout)
	for !done() {
		if time.Now().After(deadline) {
			fail("timed out waiting for %s", what)
		}
		time.Sleep(time.Second)
	}
}
