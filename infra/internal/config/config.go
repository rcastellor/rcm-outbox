// Package config agrupa la configuración de Pulumi leída desde el stack.
package config

import (
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

// DatabaseConfig agrupa la configuración de la base de datos PostgreSQL.
type DatabaseConfig struct {
	// SkipFinalSnapshot evita el snapshot final al eliminar la instancia RDS
	// (útil en dev).
	SkipFinalSnapshot bool `json:"skipFinalSnapshot"`
}

// WorkerConfig agrupa la configuración del worker de outbox. Es un subconjunto
// de lo que el usuario sobrescribe en el stack; los defaults se aplican en los
// componentes (lambda/outbox).
type WorkerConfig struct {
	// TopicName es el nombre del topic SNS FIFO al que el worker publica los
	// eventos del outbox.
	TopicName string `json:"topicName"`
	// BatchSize es el tamaño de bloque de registros que procesa cada invocación
	// del worker; el dispatcher lo usa para calcular cuántos workers lanzar.
	BatchSize int `json:"batchSize"`
	// MaxWorkers es el tope de instancias del worker que el dispatcher lanza en
	// función de los registros pendientes.
	MaxWorkers int `json:"maxWorkers"`
	// MaxAttempts es el número máximo de intentos de publicación antes de
	// marcar un registro como dead.
	MaxAttempts int `json:"maxAttempts"`
	// BackoffBaseSeconds es la base (en segundos) del backoff exponencial.
	BackoffBaseSeconds int `json:"backoffBaseSeconds"`
	// MaxBackoffSeconds es el tope (en segundos) del backoff exponencial.
	MaxBackoffSeconds int `json:"maxBackoffSeconds"`
}

// Config agrupa la configuración de la infraestructura leída desde el stack de Pulumi.
type Config struct {
	// Database es la configuración de la base de datos PostgreSQL.
	Database DatabaseConfig
	// Topics son los topics SNS a crear.
	Topics []Topic
	// Worker es la configuración del worker de outbox.
	Worker WorkerConfig
}

// LoadConfig lee la configuración del stack de Pulumi.
func LoadConfig(ctx *pulumi.Context) (*Config, error) {
	cfg := config.New(ctx, "")

	var database DatabaseConfig
	if err := cfg.GetObject("database", &database); err != nil {
		return nil, err
	}

	var topics []Topic
	if err := cfg.GetObject("topics", &topics); err != nil {
		return nil, err
	}

	var worker WorkerConfig
	if err := cfg.GetObject("worker", &worker); err != nil {
		return nil, err
	}

	return &Config{
		Database: database,
		Topics:   topics,
		Worker:   worker,
	}, nil
}
