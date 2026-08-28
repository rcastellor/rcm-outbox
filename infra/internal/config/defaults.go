package config

const (
	// DefaultWorkerTopicName es el topic SNS FIFO al que publica el worker cuando
	// no se sobrescribe en la configuración del stack.
	DefaultWorkerTopicName = "rcm-outbox-orders.fifo"
	// DefaultStatsQueueName es la cola SQS FIFO suscrita al topic de outbox de
	// órdenes que alimenta al consumidor de estadísticas.
	DefaultStatsQueueName = "rcm-outbox-orders-stats.fifo"
	// DefaultDispatchQueueName es la cola SQS estándar donde el dispatcher encola
	// los trabajos de outbox (un mensaje por worker).
	DefaultDispatchQueueName = "rcm-outbox-dispatch"
	// DefaultDispatchDLQName es la cola dead-letter de la cola de dispatch.
	DefaultDispatchDLQName = "rcm-outbox-dispatch-dlq"
	// DefaultStatsDLQName es la cola dead-letter (FIFO, como su origen) de la
	// cola de consumidores de estadísticas.
	DefaultStatsDLQName = "rcm-outbox-orders-stats-dlq.fifo"
	// DefaultMaxReceiveCount es el número de recibos fallidos antes de que la
	// redrive policy mueva un mensaje a su DLQ.
	DefaultMaxReceiveCount = 3
	// DefaultStatsTableName es la tabla DynamoDB donde el consumidor agrega los
	// items comprados por cliente, producto y día.
	DefaultStatsTableName = "rcm-outbox-stats"
	// DefaultStatsInboxTableName es la tabla DynamoDB donde el consumidor
	// registra los eventos ya procesados (patrón inbox) para deduplicar.
	DefaultStatsInboxTableName = "rcm-outbox-stats-inbox"
	// DefaultBatchSize es el tamaño de bloque de registros por invocación del worker.
	DefaultBatchSize = 10
	// DefaultMaxWorkers es el tope de instancias de worker que lanza el dispatcher.
	DefaultMaxWorkers = 20
	// DefaultMaxAttempts es el número máximo de intentos antes de marcar dead.
	DefaultMaxAttempts = 5
	// DefaultBackoffBaseSeconds es la base (segundos) del backoff exponencial.
	DefaultBackoffBaseSeconds = 60
	// DefaultMaxBackoffSeconds es el tope (segundos) del backoff exponencial.
	DefaultMaxBackoffSeconds = 480
)
