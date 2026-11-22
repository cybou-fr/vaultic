package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync/atomic"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"

	"github.com/restic/restic/internal/global"
	"github.com/restic/restic/internal/repository"
)

// vaulticRunOptions tunes one engine operation.
type vaulticRunOptions struct {
	Create          bool // the repository is being created: skip the local existence check
	AllowNoPassword bool // a reachability test may run without a password
	Inline          bool // report failures in the status line instead of a dialog
}

// vaulticRunnerHooks connects the runner to the application shell.
type vaulticRunnerHooks struct {
	Window fyne.Window
	Busy   *atomic.Bool // the single engine gate shared with the backup coordinator
	Panel  *vaulticStatusPanel
	Log    func(string)
	// SetBusy enables or disables the controls for the running state.
	SetBusy func(on bool)
	// CloseNow closes the window after a running operation ended during shutdown.
	CloseNow func()
	// CloudAuth returns the session credentials of a repository.
	CloudAuth func(repo string) vaulticCloudAuth
	// Completed runs after an operation finished without error.
	Completed func(title string)
	// RefreshLayout redraws the window after the progress area appeared.
	RefreshLayout func()
}

// vaulticEngineRunner starts one engine operation at a time on a worker
// goroutine and reports it on the UI thread: progress, status, log, errors and
// cancellation. Backups go through the coordinator and only borrow Handle.
type vaulticEngineRunner struct {
	hooks   vaulticRunnerHooks
	cancel  context.CancelFunc
	closing bool
}

func newVaulticEngineRunner(hooks vaulticRunnerHooks) *vaulticEngineRunner {
	return &vaulticEngineRunner{hooks: hooks}
}

// SetCancel registers how to stop the running operation (nil when idle).
func (r *vaulticEngineRunner) SetCancel(cancel context.CancelFunc) { r.cancel = cancel }

// Running reports whether an operation can currently be cancelled.
func (r *vaulticEngineRunner) Running() bool { return r.cancel != nil }

// Closing reports whether the window waits for the operation to stop.
func (r *vaulticEngineRunner) Closing() bool { return r.closing }

// RequestCancel stops the running operation, if any.
func (r *vaulticEngineRunner) RequestCancel() {
	if r.cancel != nil {
		// Status first: the operation reports its own end once cancelled.
		r.hooks.Panel.Status.SetText(vaulticT("Annulation…"))
		r.cancel()
	}
}

// BeginClose cancels the running operation so the window can close once it
// ended. It returns false when nothing runs and the window can close at once.
func (r *vaulticEngineRunner) BeginClose() bool {
	if r.cancel == nil {
		return false
	}
	r.closing = true
	r.hooks.Panel.Status.SetText(vaulticT("Annulation avant fermeture…"))
	r.cancel()
	return true
}

// vaulticMissingConnectionInfo reports whether an operation lacks what it
// needs: a repository and, unless allowed, its password.
func vaulticMissingConnectionInfo(repo, password string, allowNoPassword bool) bool {
	return repo == "" || (password == "" && !allowNoPassword)
}

// vaulticFriendlyEngineError replaces engine errors that need no technical
// detail by a message for the user.
func vaulticFriendlyEngineError(err error) error {
	if errors.Is(err, repository.ErrNoKeyFound) {
		return errors.New(vaulticT("Mot de passe incorrect ou aucune clé utilisable dans ce dépôt."))
	}
	return err
}

// vaulticCheckLocalRepositoryReachable fails early, with advice, when a local
// repository path cannot be read (for example an unplugged disk).
func vaulticCheckLocalRepositoryReachable(repo, resolved string) error {
	if !vaulticLocalRepository(repo) {
		return nil
	}
	if _, err := os.Stat(resolved); err != nil {
		return fmt.Errorf(vaulticT("dépôt inaccessible. Vérifiez le chemin et le branchement du disque : %w"), err)
	}
	return nil
}

// Handle processes one output line of the engine. It is called from the
// engine's goroutines; widget access is marshalled onto the UI thread. report
// (a backup) additionally records the event.
func (r *vaulticEngineRunner) Handle(ctx context.Context, title, line string, report *vaulticBackupReport, redact func(string) string) {
	if line == "" {
		return
	}
	panel := r.hooks.Panel
	var e vaulticEngineEvent
	if json.Unmarshal([]byte(line), &e) == nil {
		if report != nil {
			report.record(e)
		}
		view := vaulticInterpretEvent(e, title, report != nil)
		if view.HasProgress || view.Status != "" {
			fyne.Do(func() {
				if ctx.Err() != nil {
					return
				}
				if view.HasProgress && panel.SetProgress(view.Progress) {
					r.hooks.RefreshLayout()
				}
				if view.Status != "" {
					panel.Status.SetText(view.Status)
				}
			})
		}
		if view.Suppress {
			return
		}
		if view.Log != "" {
			line = view.Log
		}
	}
	line = redact(line)
	fyne.Do(func() { r.hooks.Log(line) })
}

// Run starts fn on a worker goroutine with engine options for repo, then runs
// finish on the UI thread when fn succeeded. Only one operation may run.
func (r *vaulticEngineRunner) Run(title, repo, password string, opts vaulticRunOptions, fn func(context.Context, global.Options) error, finish func() error) {
	h := r.hooks
	w := h.Window
	if vaulticMissingConnectionInfo(repo, password, opts.AllowNoPassword) {
		dialog.ShowInformation(vaulticT("Informations manquantes"), vaulticT("Indiquez un dépôt et son mot de passe."), w)
		return
	}
	if !h.Busy.CompareAndSwap(false, true) {
		dialog.ShowInformation(vaulticT("Opération en cours"), vaulticT("Une seule opération est autorisée à la fois."), w)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	h.SetBusy(true)
	h.Panel.Progress.SetValue(0)
	h.Panel.Status.SetText(title + "…")
	h.Log(vaulticT("Début : ") + title)
	cloudAuth := h.CloudAuth(repo)
	redact := vaulticRedactCloudSecrets(vaulticRedactOperation(password), cloudAuth)
	go func() {
		terminal := &vaulticTerminal{line: func(line string) { r.Handle(ctx, title, line, nil, redact) }, output: io.Discard}
		gopts := global.Options{
			Repo: vaulticResolvedRepository(repo), Password: password, Backends: vaulticBackendsForSession(cloudAuth),
			Term: terminal, JSON: true, Verbosity: 1,
		}
		var err error
		if !opts.Create && !opts.Inline {
			err = vaulticCheckLocalRepositoryReachable(repo, gopts.Repo)
		}
		if err == nil {
			err = fn(ctx, gopts)
		}
		fyne.Do(func() {
			cancel()
			r.cancel = nil
			h.Busy.Store(false)
			err = vaulticFriendlyEngineError(err)
			if r.closing {
				if err == nil && finish != nil {
					_ = finish()
				}
				h.CloseNow()
				return
			}
			h.SetBusy(false)
			status := h.Panel.Status
			if errors.Is(err, context.Canceled) {
				status.SetText(title + vaulticT(" — annulé"))
				h.Log(vaulticT("Annulé : ") + title)
				return
			}
			if err == nil && finish != nil {
				err = finish()
			}
			if err != nil {
				message := redact(err.Error())
				if opts.Inline {
					status.SetText(message)
				} else {
					status.SetText(vaulticT("Erreur : ") + title)
				}
				h.Log(message)
				if !opts.Inline {
					dialog.ShowError(errors.New(message), w)
				}
				return
			}
			status.SetText(title + vaulticT(" — terminé"))
			h.Completed(title)
			h.Log(vaulticT("Terminé : ") + title)
		})
	}()
}
