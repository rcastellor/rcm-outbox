package config

import (
	"errors"
	"os"
	"strconv"
	"time"

	platformconfig "github.com/rcastellor/rcm-outbox/rcm-platform/config"
)

const defaultBatchSize = 10
const defaultMaxAttempts = 5
const defaultBackoffBaseSeconds = 60
const defaultMaxBackoffSeconds = 480

// Config agrupa la configuración de la Lambda del worker leída del entorno.
type Config struct {
	// DB es la configuración de conexión a la base de datos (compartida con el
	// resto de Lambdas vía rcm-platform).
	DB *platformconfig.DBConfig
	// SNSTopicARN es el ARN del topic SNS FIFO donde se publican los eventos.
	SNSTopicARN string
	// BatchSize es el tamaño de bloque de registros a procesar por invocación.
	BatchSize int
	// MaxAttempts es el número máximo de intentos de publicación antes de
	// marcar un registro como dead.
	MaxAttempts int
	// BackoffBase es la base del backoff exponencial para los reintentos.
	BackoffBase time.Duration
	// MaxBackoff es el tope del backoff exponencial.
	MaxBackoff time.Duration
}

// Load lee la configuración desde las variables de entorno.
func Load() (*Config, error) {
	db, err := platformconfig.LoadDB()
	if err != nil {
		return nil, err
	}

	topicARN := os.Getenv("SNS_TOPIC_ARN")
	if topicARN == "" {
		return nil, errors.New("variable de entorno SNS_TOPIC_ARN no definida")
	}

	batchSize, err := positiveInt("BATCH_SIZE", defaultBatchSize)
	if err != nil {
		return nil, err
	}

	maxAttempts, err := positiveInt("MAX_ATTEMPTS", defaultMaxAttempts)
	if err != nil {
		return nil, err
	}

	backoffBase, err := positiveSeconds("BACKOFF_BASE_SECONDS", defaultBackoffBaseSeconds)
	if err != nil {
		return nil, err
	}

	maxBackoff, err := positiveSeconds("MAX_BACKOFF_SECONDS", defaultMaxBackoffSeconds)
	if err != nil {
		return nil, err
	}

	return &Config{
		DB:          db,
		SNSTopicARN: topicARN,
		BatchSize:   batchSize,
		MaxAttempts: maxAttempts,
		BackoffBase: backoffBase,
		MaxBackoff:  maxBackoff,
	}, nil
}

// positiveInt lee una variable de entorno entera positiva, devolviendo el
// default si no está definida.
func positiveInt(name string, def int) (int, error) {
	if v := os.Getenv(name); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return 0, errors.New("variable de entorno " + name + " inválida")
		}
		return n, nil
	}
	return def, nil
}

// positiveSeconds lee una variable de entorno en segundos positiva y la
// devuelve como time.Duration, usando el default si no está definida.
func positiveSeconds(name string, defSeconds int) (time.Duration, error) {
	if v := os.Getenv(name); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return 0, errors.New("variable de entorno " + name + " inválida")
		}
		return time.Duration(n) * time.Second, nil
	}
	return time.Duration(defSeconds) * time.Second, nil
}
