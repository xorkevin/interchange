## interchange completion fish

Generate the autocompletion script for fish

### Synopsis

Generate the autocompletion script for the fish shell.

To load completions in your current shell session:

	interchange completion fish | source

To load completions for every new session, execute once:

	interchange completion fish > ~/.config/fish/completions/interchange.fish

You will need to start a new shell for this setup to take effect.


```
interchange completion fish [flags]
```

### Options

```
  -h, --help              help for fish
      --no-descriptions   disable completion descriptions
```

### Options inherited from parent commands

```
      --log-level string   log level (default "info")
```

### SEE ALSO

* [interchange completion](interchange_completion.md)	 - Generate the autocompletion script for the specified shell

