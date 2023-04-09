## interchange completion zsh

Generate the autocompletion script for zsh

### Synopsis

Generate the autocompletion script for the zsh shell.

If shell completion is not already enabled in your environment you will need
to enable it.  You can execute the following once:

	echo "autoload -U compinit; compinit" >> ~/.zshrc

To load completions in your current shell session:

	source <(interchange completion zsh)

To load completions for every new session, execute once:

#### Linux:

	interchange completion zsh > "${fpath[1]}/_interchange"

#### macOS:

	interchange completion zsh > $(brew --prefix)/share/zsh/site-functions/_interchange

You will need to start a new shell for this setup to take effect.


```
interchange completion zsh [flags]
```

### Options

```
  -h, --help              help for zsh
      --no-descriptions   disable completion descriptions
```

### Options inherited from parent commands

```
      --log-level string   log level (default "info")
```

### SEE ALSO

* [interchange completion](interchange_completion.md)	 - Generate the autocompletion script for the specified shell

