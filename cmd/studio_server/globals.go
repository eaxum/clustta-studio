package main

import "clustta/internal/integration_listener"

// ListenerManager owns the in-process integration listener for this studio.
// Nil until startServer initialises it from CONFIG.
var ListenerManager *integration_listener.Manager
