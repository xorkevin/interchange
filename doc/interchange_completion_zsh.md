## interchange completion zsh

generate the autocompletion script for zsh

### Synopsis


Generate the autocompletion script for the zsh shell.

If shell completion is not already enabled in your environment you will need
to enable it.  You can execute the following once:

$ echo "autoload -U compinit; compinit" >> ~/.zshrc

To load completions for every new session, execute once:
# Linux:
$ interchange completion zsh > "${fpath[1]}/_interchange"
# macOS:
$ interchange completion zsh > /usr/local/share/zsh/site-functions/_interchange

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
      --config string   config file (default is $XDG_CONFIG_HOME/.interchange.yaml)
      --debug           turn on debug output
```

### SEE ALSO

* [interchange completion](interchange_completion.md)	 - generate the autocompletion script for the specified shell

