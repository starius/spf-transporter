# connmonitor
`connmonitor` is a light weight bandwidth monitor

## Usage
To use `connmonitor`, add a `Monitor` to the interface that you would like to
have the bandwidth monitored. Next, use the `NewMonitorConn()` method to wrap
any `net.Conn` connections that the interface creates or uses. This allows the
`Monitor` to count any bytes passed through the `Write` and `Read` commands of
the connection.

To see the current bandwidth usage, call `Counts()`, and the total bytes written
and read will be returned.
