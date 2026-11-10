// Command mockoidc is a fake Google OIDC provider for make dev and E2E.
// It listens on 127.0.0.1:9999. ID tokens use issuer http://localhost:9999/oidc
// because that is github.com/oauth2-proxy/mockoidc's issuer base.
// The default user is dev@example.com with hd=example.com. POST /_mock/next-user
// queues a different identity for the next successful login.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const (
	listenAddr = "127.0.0.1:9999"
	// publicHost is the host in iss and in the discovery document.
	// It is localhost, not the bind address, so OIDC_ISSUER can stay stable.
	publicHost = "localhost:9999"

	// devClientID and devClientSecret are the fixed fake client for this local
	// IdP. They are not credentials for a real Google app.
	devClientID     = "arc-dev"
	devClientSecret = "arc-dev-secret" //nolint:gosec // G101: public fake secret for the local mock IdP
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, listenAddr, publicHost, log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, listen, host string, log *slog.Logger) error {
	srv, err := newServer(ctx, listen, host, log)
	if err != nil {
		return err
	}
	log.Info("listening", "addr", listen, "issuer", srv.issuer(), "client_id", devClientID)

	select {
	case err := <-srv.serveErr:
		srv.wg.Wait()
		return err
	case <-ctx.Done():
	}

	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = srv.shutdown(shutdownCtx)
	select {
	case serveErr := <-srv.serveErr:
		return errors.Join(err, serveErr)
	default:
		return err
	}
}
