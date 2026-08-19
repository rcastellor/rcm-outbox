package logger

import (
	"log/slog"
	"os"
)

// New devuelve un logger JSON configurado para las Lambdas.
func New() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
}
