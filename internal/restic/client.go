// Package restic invokes the official restic binary without a shell.
// It deliberately never persists passwords or passes them on the command line.
package restic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Client struct {
	Binary     string
	Repository string
	Password   string
}

type Progress struct {
	MessageType string  `json:"message_type"`
	PercentDone float64 `json:"percent_done"`
	TotalFiles  int64   `json:"total_files"`
	FilesDone   int64   `json:"files_done"`
	TotalBytes  int64   `json:"total_bytes"`
	BytesDone   int64   `json:"bytes_done"`
	SnapshotID  string  `json:"snapshot_id"`
	ErrorCount  int64   `json:"error_count"`
}

type Snapshot struct {
	Time     time.Time `json:"time"`
	ID       string    `json:"id"`
	ShortID  string    `json:"short_id"`
	Hostname string    `json:"hostname"`
	Paths    []string  `json:"paths"`
	Tags     []string  `json:"tags"`
}

type Event struct {
	Progress *Progress
	Line     string
}

func (c Client) binary() string {
	name := strings.TrimSpace(c.Binary)
	if name == "" {
		name = "restic"
	}
	// A copy of restic placed beside the GUI makes portable SSD installs easy.
	if name == "restic" || name == "restic.exe" {
		exe, err := os.Executable()
		if err == nil {
			candidate := filepath.Join(filepath.Dir(exe), name)
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
			if name == "restic" {
				candidate = filepath.Join(filepath.Dir(exe), "restic.exe")
				if _, err := os.Stat(candidate); err == nil {
					return candidate
				}
			}
		}
	}
	return name
}

// A relative local repository is resolved relative to the app executable,
// not the process working directory, so moving an SSD keeps it portable.
func (c Client) repositoryPath() string {
	repo := strings.TrimSpace(c.Repository)
	if repo == "" || !isLocalRepo(repo) || filepath.IsAbs(repo) {
		return repo
	}
	exe, err := os.Executable()
	if err != nil {
		return repo
	}
	return filepath.Join(filepath.Dir(exe), repo)
}

func (c Client) Validate() error {
	if strings.TrimSpace(c.Repository) == "" {
		return errors.New("indiquez le chemin du dépôt restic")
	}
	if c.Password == "" {
		return errors.New("saisissez le mot de passe du dépôt")
	}
	if strings.ContainsRune(c.Repository, '\x00') || strings.ContainsRune(c.Password, '\x00') {
		return errors.New("caractère non valide dans le chemin ou le mot de passe")
	}
	return nil
}

func (c Client) command(ctx context.Context, args ...string) (*exec.Cmd, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	// --repo and --json are global flags. Each operation uses the same backend.
	all := append([]string{"--repo", c.repositoryPath()}, args...)
	cmd := exec.CommandContext(ctx, c.binary(), all...)
	// Remove inherited password-related variables so this UI password wins.
	env := make([]string, 0, len(os.Environ())+2)
	for _, v := range os.Environ() {
		key, _, _ := strings.Cut(v, "=")
		switch strings.ToUpper(key) {
		case "RESTIC_PASSWORD", "RESTIC_PASSWORD_FILE", "RESTIC_PASSWORD_COMMAND":
			continue
		}
		env = append(env, v)
	}
	cmd.Env = append(env, "RESTIC_PASSWORD="+c.Password, "RESTIC_PROGRESS_FPS=4")
	return cmd, nil
}

func trimError(stderr []byte, err error) error {
	msg := strings.TrimSpace(string(stderr))
	if len(msg) > 1800 {
		msg = msg[len(msg)-1800:]
	}
	if msg == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, msg)
}

func (c Client) Version(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, c.binary(), "version")
	data, err := cmd.CombinedOutput()
	if err != nil {
		return "", trimError(data, err)
	}
	return strings.TrimSpace(string(data)), nil
}

func (c Client) Init(ctx context.Context, emit func(Event)) error {
	return c.stream(ctx, emit, "init")
}

func (c Client) Check(ctx context.Context, emit func(Event)) error {
	return c.stream(ctx, emit, "check")
}

func (c Client) Backup(ctx context.Context, sources []string, emit func(Event)) error {
	if len(sources) == 0 {
		return errors.New("sélectionnez au moins un dossier ou fichier")
	}
	clean := make([]string, 0, len(sources))
	for _, path := range sources {
		if strings.TrimSpace(path) == "" {
			return errors.New("chemin source vide")
		}
		full, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		if _, err := os.Stat(full); err != nil {
			return fmt.Errorf("source %q : %w", full, err)
		}
		// Prevent recursively backing up the repository when using a local path.
		if isLocalRepo(c.Repository) {
			repo, err := filepath.Abs(c.repositoryPath())
			if err != nil {
				return err
			}
			if sameOrWithin(full, repo) {
				return fmt.Errorf("le dépôt %q se trouve dans la source %q", repo, full)
			}
		}
		clean = append(clean, full)
	}
	args := append([]string{"backup", "--json", "--"}, clean...)
	return c.stream(ctx, emit, args...)
}

func isLocalRepo(repo string) bool {
	// restic's remote backends use a scheme prefix; Windows drive letters are local.
	return !strings.Contains(repo, ":") || (len(repo) >= 2 && repo[1] == ':' && ((repo[0] >= 'A' && repo[0] <= 'Z') || (repo[0] >= 'a' && repo[0] <= 'z')))
}

func sameOrWithin(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))))
}

func (c Client) Snapshots(ctx context.Context) ([]Snapshot, error) {
	cmd, err := c.command(ctx, "snapshots", "--json")
	if err != nil {
		return nil, err
	}
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return nil, trimError(exit.Stderr, err)
		}
		return nil, err
	}
	var snaps []Snapshot
	if err := json.Unmarshal(out, &snaps); err != nil {
		return nil, fmt.Errorf("décodage JSON des instantanés : %w", err)
	}
	return snaps, nil
}

var snapshotID = regexp.MustCompile(`^[0-9a-fA-F]{8,64}$`)

func (c Client) Restore(ctx context.Context, id, destination string, emit func(Event)) error {
	if !snapshotID.MatchString(id) {
		return errors.New("identifiant d'instantané non valide")
	}
	if strings.TrimSpace(destination) == "" {
		return errors.New("sélectionnez un dossier de restauration")
	}
	target, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(target)
	if err == nil && len(entries) != 0 {
		return errors.New("pour votre sécurité, sélectionnez un dossier de restauration vide")
	}
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("vérification du dossier : %w", err)
	}
	// Always put restored data into an empty directory to avoid overwrites.
	return c.stream(ctx, emit, "restore", id, "--target", target)
}

func (c Client) stream(ctx context.Context, emit func(Event), args ...string) error {
	cmd, err := c.command(ctx, args...)
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	var wg sync.WaitGroup
	var stderrText bytes.Buffer
	wg.Add(2)
	go func() {
		defer wg.Done()
		scan(stdout, func(line string) {
			var p Progress
			if json.Unmarshal([]byte(line), &p) == nil && p.MessageType != "" {
				if emit != nil {
					emit(Event{Progress: &p})
				}
			} else if emit != nil {
				emit(Event{Line: line})
			}
		})
	}()
	go func() {
		defer wg.Done()
		scan(stderr, func(line string) {
			if stderrText.Len() < 4096 {
				stderrText.WriteString(line + "\n")
			}
			if emit != nil {
				emit(Event{Line: line})
			}
		})
	}()
	wg.Wait()
	err = cmd.Wait()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return trimError(stderrText.Bytes(), err)
	}
	return nil
}

func scan(r io.Reader, onLine func(string)) {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 64*1024), 2*1024*1024)
	for s.Scan() {
		onLine(s.Text())
	}
	if err := s.Err(); err != nil {
		onLine("Erreur de lecture de la sortie de restic : " + err.Error())
	}
}
