package esl

import (
	"context"
	"errors"
	"net"
	"strconv"
	"time"

	"gofreeswitchesl/store"

	"github.com/0x19/goesl"
	"github.com/sirupsen/logrus"
)

// Client wraps the goesl client and handles ESL events
type Client struct {
	conn      *goesl.Client
	log       *logrus.Logger
	store     *store.Store
	addr      string // Expected format: "host:port"
	pass      string
	reconnect chan struct{}
}

var ErrESLNotConnected = errors.New("ESL client not connected") // Custom error

// NewClient creates a new ESL client
func NewClient(addr, pass string, s *store.Store, logger *logrus.Logger) *Client {
	return &Client{
		log:       logger,
		store:     s,
		addr:      addr,
		pass:      pass,
		reconnect: make(chan struct{}, 1), // Buffered channel to prevent blocking on initial signal
	}
}

// connect establishes a connection to FreeSWITCH ESL
func (c *Client) connect(_ context.Context) error {
	host, portStr, err := net.SplitHostPort(c.addr)
	if err != nil {
		c.log.WithError(err).Error("Invalid ESL_ADDR format. Expected host:port")
		return err
	}
	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		c.log.WithError(err).Error("Invalid ESL_PORT in ESL_ADDR.")
		return err
	}

	client, err := goesl.NewClient(host, uint(port), c.pass, 10) // Attempting to match (string, uint, string, int)
	if err != nil {
		c.log.WithError(err).Error("Failed to connect to FreeSWITCH ESL")
		return err
	}
	c.conn = client
	go client.Handle() // Start background handler for incoming events
	c.log.Info("Successfully connected to FreeSWITCH ESL and started handler")
	return nil
}

// Start connects to FreeSWITCH and starts handling events
func (c *Client) Start(ctx context.Context) error {
	c.log.Info("Starting ESL client...")

	// Initial connection attempt
	if err := c.connect(ctx); err != nil {
		c.log.WithError(err).Error("Initial ESL connection failed. Will retry in background.")
		c.reconnect <- struct{}{}
	} else {
		if err := c.subscribeToEvents(); err != nil {
			c.log.WithError(err).Error("Failed to subscribe to ESL events after initial connection")
			c.reconnect <- struct{}{}
		}
	}

	go c.eventLoop(ctx)
	go c.reconnectionManager(ctx)

	return nil
}

// reconnectionManager handles attempts to reconnect to ESL if the connection is lost.
func (c *Client) reconnectionManager(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second) // Retry every 15 seconds
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.log.Info("Reconnection manager stopping due to context cancellation.")
			return
		case <-c.reconnect:
			c.log.Info("Attempting to reconnect to ESL...")
			if c.conn != nil {
				c.conn.Close() // Close existing connection before creating a new one
				c.conn = nil
			}
			if err := c.connect(ctx); err != nil {
				c.log.WithError(err).Error("ESL reconnection attempt failed. Will retry.")
				go func() {
					time.Sleep(5 * time.Second)
					c.reconnect <- struct{}{}
				}()
			} else {
				c.log.Info("ESL reconnected successfully.")
				if err := c.subscribeToEvents(); err != nil {
					c.log.WithError(err).Error("Failed to subscribe to ESL events after reconnection")
					c.reconnect <- struct{}{}
				}
			}
		case <-ticker.C:
			if c.conn == nil {
				c.log.Warn("ESL connection is nil, triggering reconnect.")
				c.reconnect <- struct{}{}
			}
		}
	}
}

// eventLoop listens for and processes ESL events
func (c *Client) eventLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			c.log.Info("ESL event loop stopping due to context cancellation.")
			return
		default:
			if c.conn == nil {
				c.log.Warn("ESL connection not available, pausing event processing.")
				time.Sleep(5 * time.Second) // Wait before checking connection again
				continue
			}

			msg, err := c.conn.ReadMessage()
			if err != nil {
				c.log.WithError(err).Error("Error reading ESL message")
				c.reconnect <- struct{}{}
				time.Sleep(1 * time.Second)
				continue
			}

			if msg == nil {
				continue // Should not happen with ReadMessage, but good practice
			}

			go c.handleEvent(ctx, msg) // Handle event in a new goroutine
		}
	}
}

// subscribeToEvents subscribes to necessary ESL events
func (c *Client) subscribeToEvents() error {
	if c.conn == nil {
		return ErrESLNotConnected // Use custom error
	}
	// Subscribe to ALL events for debugging
	if err := c.conn.Send("event json ALL"); err != nil {
		c.log.WithError(err).Error("Failed to send event subscription command to ESL")
		return err
	}
	c.log.Info("Subscribed to ALL ESL events (debug mode)")
	return nil
}

// handleEvent processes a single ESL event
func (c *Client) handleEvent(ctx context.Context, msg *goesl.Message) {
	eventName := msg.GetHeader("Event-Name")
	uuid := msg.GetHeader("Unique-ID")

	if uuid == "" {
		// Only log relevant events with no Unique-ID at info, skip debug logs for others
		if eventName == "CHANNEL_CREATE" || eventName == "CHANNEL_HANGUP" {
			c.log.WithField("eventName", eventName).Info("Received relevant event with no Unique-ID, skipping")
		}
		return
	}

	// Log full message for relevant events at INFO level for visibility
	if eventName == "CHANNEL_CREATE" || eventName == "CHANNEL_HANGUP" {
		c.log.WithFields(logrus.Fields{
			"eventName":   eventName,
			"uuid":        uuid,
			"fullMessage": msg.String(), // msg.String() provides a well-formatted representation
		}).Info("Attempting to process ESL event")
	}

	switch eventName {
	case "CHANNEL_CREATE":
		c.handleChannelCreate(ctx, msg, uuid)
	case "CHANNEL_HANGUP":
		c.handleChannelHangup(ctx, msg, uuid)
	default:
		// Already logged at debug if it's not one of the above
	}
}

// handleChannelCreate handles the CHANNEL_CREATE event
func (c *Client) handleChannelCreate(ctx context.Context, msg *goesl.Message, uuid string) {
	c.log.WithField("uuid", uuid).Info("Handling CHANNEL_CREATE event")

	startTimeStr := msg.GetHeader("Event-Date-Timestamp")
	if startTimeStr == "" {
		c.log.WithField("uuid", uuid).Error("Event-Date-Timestamp is missing for CHANNEL_CREATE")
		return
	}
	startTimeUnix, err := strconv.ParseInt(startTimeStr, 10, 64)
	if err != nil {
		c.log.WithError(err).WithFields(logrus.Fields{
			"uuid":           uuid,
			"timestampValue": startTimeStr,
		}).Error("Failed to parse start time for CHANNEL_CREATE")
		return
	}

	call := &store.Call{
		UUID:      uuid,
		Direction: msg.GetHeader("Call-Direction"),
		Caller:    msg.GetHeader("Caller-Caller-ID-Number"),
		Callee:    msg.GetHeader("Caller-Destination-Number"),
		StartTime: time.Unix(startTimeUnix/1000000, (startTimeUnix%1000000)*1000), // Convert microseconds to Time
	}

	// Log the call object before attempting to save
	c.log.WithFields(logrus.Fields{
		"uuid":      call.UUID,
		"direction": call.Direction,
		"caller":    call.Caller,
		"callee":    call.Callee,
		"startTime": call.StartTime,
	}).Info("Parsed call data for CHANNEL_CREATE")

	if err := c.store.CreateCall(ctx, call); err != nil {
		c.log.WithError(err).WithField("uuid", uuid).Error("Failed to create call record from CHANNEL_CREATE")
	} else {
		c.log.WithField("uuid", uuid).Info("Successfully created call record from CHANNEL_CREATE")
	}
}

// handleChannelHangup handles the CHANNEL_HANGUP event
func (c *Client) handleChannelHangup(ctx context.Context, msg *goesl.Message, uuid string) {
	c.log.WithField("uuid", uuid).Info("Handling CHANNEL_HANGUP event")

	hangupTimeStr := msg.GetHeader("Event-Date-Timestamp")
	if hangupTimeStr == "" {
		c.log.WithField("uuid", uuid).Error("Event-Date-Timestamp is missing for CHANNEL_HANGUP")
		return
	}
	hangupTimeUnix, err := strconv.ParseInt(hangupTimeStr, 10, 64)
	if err != nil {
		c.log.WithError(err).WithFields(logrus.Fields{
			"uuid":           uuid,
			"timestampValue": hangupTimeStr,
		}).Error("Failed to parse hangup time for CHANNEL_HANGUP")
		return
	}
	endTime := time.Unix(hangupTimeUnix/1000000, (hangupTimeUnix%1000000)*1000)
	status := msg.GetHeader("Hangup-Cause")

	// Log the data before attempting to update
	c.log.WithFields(logrus.Fields{
		"uuid":    uuid,
		"endTime": endTime,
		"status":  status,
	}).Info("Parsed hangup data for CHANNEL_HANGUP")

	if err := c.store.UpdateCallHangup(ctx, uuid, endTime, status); err != nil {
		c.log.WithError(err).WithField("uuid", uuid).Error("Failed to update call record from CHANNEL_HANGUP")
	} else {
		c.log.WithField("uuid", uuid).Info("Successfully updated call record from CHANNEL_HANGUP")
	}
}

// Close gracefully closes the ESL connection
func (c *Client) Close() error {
	c.log.Info("Closing ESL client connection...")
	if c.conn != nil {
		return c.conn.Close()
	}
	c.log.Info("ESL connection already closed or not established.")
	return nil
}
