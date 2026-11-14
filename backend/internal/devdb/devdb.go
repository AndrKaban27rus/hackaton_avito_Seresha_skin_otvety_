// Package devdb starts a local PostgreSQL without Docker for development and
// tests. The binaries are downloaded once into the user cache.
package devdb

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
)

type DB struct {
	pg  *embeddedpostgres.EmbeddedPostgres
	URL string
}

// Start runs PostgreSQL with its data in dataDir. Port 0 picks a free port.
func Start(dataDir string, port uint32, logs io.Writer) (*DB, error) {
	if port == 0 {
		free, err := freePort()
		if err != nil {
			return nil, err
		}
		port = free
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	// Keep runtime files next to the data: parallel instances must not share them.
	cfg := embeddedpostgres.DefaultConfig().
		Version(embeddedpostgres.V17).
		Port(port).
		Database("buddy").Username("buddy").Password("buddy").
		DataPath(filepath.Join(dataDir, "data")).
		RuntimePath(filepath.Join(dataDir, "runtime")).
		Locale("C").Encoding("UTF8").
		Logger(logs)
	stopOrphan(dataDir)
	pg := embeddedpostgres.NewDatabase(cfg)
	if err := pg.Start(); err != nil {
		return nil, fmt.Errorf("start local PostgreSQL: %w", err)
	}
	url := fmt.Sprintf("postgres://buddy:buddy@127.0.0.1:%d/buddy?sslmode=disable", port)
	return &DB{pg: pg, URL: url}, nil
}

func (d *DB) Stop() error { return d.pg.Stop() }

// stopOrphan stops a server left running by a killed previous run on the same data.
func stopOrphan(dataDir string) {
	data := filepath.Join(dataDir, "data")
	if _, err := os.Stat(filepath.Join(data, "postmaster.pid")); err != nil {
		return
	}
	name := "pg_ctl"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	pgCtl := filepath.Join(dataDir, "runtime", "bin", name)
	if _, err := os.Stat(pgCtl); err != nil {
		return
	}
	_ = exec.Command(pgCtl, "stop", "-D", data, "-m", "fast", "-w").Run()
}

func freePort() (uint32, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return uint32(l.Addr().(*net.TCPAddr).Port), nil
}

// DefaultDir is outside the repository and has an ASCII path, which initdb needs on Windows.
func DefaultDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "ai-buddy", "pg"), nil
}
