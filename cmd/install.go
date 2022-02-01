package cmd

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/b4b4r07/afx/pkg/config"
	"github.com/b4b4r07/afx/pkg/templates"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

type installCmd struct {
	meta
}

var (
	// installLong is long description of fmt command
	installLong = templates.LongDesc(``)

	// installExample is examples for fmt command
	installExample = templates.Examples(`
		# Normal
		afx install
	`)
)

// newInstallCmd creates a new fmt command
func newInstallCmd() *cobra.Command {
	c := &installCmd{}

	installCmd := &cobra.Command{
		Use:                   "install",
		Short:                 "Resume installation from paused part (idempotency)",
		Long:                  installLong,
		Example:               installExample,
		Aliases:               []string{"i"},
		DisableFlagsInUseLine: true,
		SilenceUsage:          true,
		SilenceErrors:         true,
		Args:                  cobra.MaximumNArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := c.meta.init(args); err != nil {
				return err
			}
			if c.parseErr != nil {
				return c.parseErr
			}
			c.Env.Ask(
				"AFX_SUDO_PASSWORD",
				"GITHUB_TOKEN",
			)
			return c.run(args)
		},
	}

	return installCmd
}

func (c *installCmd) run(args []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	eg := errgroup.Group{}

	limit := make(chan struct{}, 16)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, os.Interrupt)
	defer func() {
		signal.Stop(sigCh)
		cancel()
	}()

	// TODO: Check if this process does not matter other concerns
	pkgs := append(c.State.Additions, c.State.Readditions...)
	if len(pkgs) == 0 {
		// TODO: improve message
		log.Printf("[INFO] No packages to install")
		return nil
	}

	progress := config.NewProgress(pkgs)
	completion := make(chan config.Status)

	go func() {
		progress.Print(completion)
	}()

	log.Printf("[DEBUG] start to run each pkg.Install()")
	for _, pkg := range pkgs {
		pkg := pkg
		eg.Go(func() error {
			limit <- struct{}{}
			defer func() { <-limit }()
			err := pkg.Install(ctx, completion)
			switch err {
			case nil:
				c.State.Add(pkg)
			default:
				log.Printf("[DEBUG] uninstall %q because installation failed", pkg.GetName())
				pkg.Uninstall(ctx)
			}
			return err
		})
	}

	errCh := make(chan error, 1)

	go func() {
		errCh <- eg.Wait()
	}()

	var exit error
	select {
	case err := <-errCh:
		if err != nil {
			log.Printf("[ERROR] failed to install: %s\n", err)
		}
		exit = err
	case <-sigCh:
		cancel()
		log.Println("[INFO] canceled by signal")
	case <-ctx.Done():
		log.Println("[INFO] done")
	}

	defer func(err error) {
		if err != nil {
			c.Env.Refresh()
		}
	}(exit)

	return exit
}
