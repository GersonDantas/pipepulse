package testsupport

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func StartPostgres(t testing.TB) string {
	t.Helper()
	if os.Getenv("PIPEPULSE_INTEGRATION") != "1" {
		t.Skip("set PIPEPULSE_INTEGRATION=1 to run PostgreSQL integration tests")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("Docker is required for PostgreSQL integration tests")
	}

	name := fmt.Sprintf("pipepulse-postgres-%d", time.Now().UnixNano())
	command := exec.Command(
		"docker", "run", "-d", "--rm", "--name", name,
		"-e", "POSTGRES_USER=pipepulse",
		"-e", "POSTGRES_PASSWORD=pipepulse",
		"-e", "POSTGRES_DB=pipepulse",
		"-p", "127.0.0.1::5432",
		"postgres:16-alpine",
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("start PostgreSQL container: %v: %s", err, output)
	}
	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "-f", name).Run()
	})

	var port string
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		output, err := exec.Command("docker", "port", name, "5432/tcp").Output()
		if err == nil {
			address := strings.TrimSpace(string(output))
			if index := strings.LastIndex(address, ":"); index >= 0 {
				port = address[index+1:]
			}
		}
		if port != "" && postgresReadyTwice(name) {
			return fmt.Sprintf("postgres://pipepulse:pipepulse@127.0.0.1:%s/pipepulse?sslmode=disable", port)
		}
		time.Sleep(100 * time.Millisecond)
	}

	t.Fatalf("PostgreSQL container %s did not become ready", name)
	return ""
}

func postgresReadyTwice(name string) bool {
	if exec.Command("docker", "exec", name, "pg_isready", "-U", "pipepulse", "-d", "pipepulse").Run() != nil {
		return false
	}
	time.Sleep(200 * time.Millisecond)
	return exec.Command("docker", "exec", name, "pg_isready", "-U", "pipepulse", "-d", "pipepulse").Run() == nil
}
