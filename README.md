Logstreamer Directory Input
=======================  
Dynamic plugin loader for Heka's LogstreamerInput plugins

Plugin Name: **LogstreamerDirectoryInput**

The LogstreamerDirectoryInput is largely based on Heka's [ProcessDirectoryInput](https://hekad.readthedocs.io/en/latest/config/inputs/processdir.html).  
It periodically scans a filesystem directory looking
for LogstreamerInput configuration files. The LogstreamerDirectoryInput will maintain
a pool of running LogstreamerInputs based on the contents of this directory,
refreshing the set of running inputs as needed with every rescan. This allows
Heka administrators to manage a set of logstreamers for a running
hekad server without restarting the server.

Each LogstreamerDirectoryInput has a `logstreamer_dir` configuration setting, which is
the root folder of the tree where scheduled jobs are defined.
This folder must contain TOML files which specify the details
regarding which Logstreamer to run.

For example, a logstreamer_dir might look like this::


  - /usr/share/heka/logstreamers.d/
    - syslog.toml
    - apache.toml
    - mysql.toml
    - docker.toml

The names for each Logstreamer input must be unique. Any duplicate named configs
will not be loaded.  
Ex.  

	[syslog]  
	type = "LogstreamerInput"  
	and  
	[syslog2]  
	type = "LogstreamerInput"


Each config file must have a '.toml' extension. Each file which meets these criteria,
such as those shown in the example above, should contain the TOML configuration for exactly one
[LogstreamerInput](https://hekad.readthedocs.io/en/latest/config/inputs/logstreamer.html),
matching that of a standalone LogstreamerInput with
the following restrictions:

- The section name OR type *must* be `LogstreamerInput`. Any TOML sections named anything
  other than LogstreamerInput will be ignored.


Config:

- ticker_interval (int, optional):
    Amount of time, in seconds, between scans of the logstreamer_dir. Defaults to
    300 (i.e. 5 minutes).
- logstreamer_dir (string, optional):
    This is the root folder of the tree where the scheduled jobs are defined.
    Absolute paths will be honored, relative paths will be computed relative to
    Heka's globally specified share_dir. Defaults to "logstreamers.d" (i.e.
    "$share_dir/logstreamers.d").
- retries (RetryOptions, optional):
    A sub-section that specifies the settings to be used for restart behavior
    of the LogstreamerDirectoryInput (not the individual ProcessInputs, which are
    configured independently).
    See [Configuring Restarting Behavior](https://hekad.readthedocs.io/en/latest/config/index.html#configuring-restarting)

Example:

	[LogstreamerDirectoryInput]
	logstreamer_dir = "/usr/share/heka/logstreamers.d"
	ticker_interval = 120

To Build
========

  See [Building *hekad* with External Plugins](http://hekad.readthedocs.org/en/latest/installing.html#build-include-externals)
  for compiling in plugins.

  Edit cmake/plugin_loader.cmake file and add

      add_external_plugin(git https://github.com/michaelgibson/heka-logstreamer-directory-input master)

  Build Heka:
  	. ./build.sh


