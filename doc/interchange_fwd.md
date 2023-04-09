## interchange fwd

Forwards tcp and udp traffic

### Synopsis

Forwards tcp and udp traffic

```
interchange fwd [flags]
```

### Options

```
  -h, --help                   help for fwd
  -t, --tcp stringArray        forward tcp ports (SRCPORT:TARGETHOST:TARGETPORT); may be specified multiple times
      --tcp-timeout duration   tcp conn timeout (default 10s)
  -u, --udp stringArray        forward udp ports (SRCPORT:TARGETHOST:TARGETPORT); may be specified multiple times
      --udp-timeout duration   udp conn timeout (default 10s)
```

### Options inherited from parent commands

```
      --log-level string   log level (default "info")
```

### SEE ALSO

* [interchange](interchange.md)	 - A tcp and udp forwarder

