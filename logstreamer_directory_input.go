package logstreamer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"github.com/bbangert/toml"
	. "github.com/mozilla-services/heka/pipeline"
)

type LogstreamerEntry struct {
	ir     InputRunner
	maker  MutableMaker
	config *LogstreamerInputConfig
	fileName string
}

type LogstreamerDirectoryInputConfig struct {
	// Root folder of the tree where the scheduled jobs are defined.
	LogstreamerDir string `toml:"logstreamer_dir"`

	// Number of seconds to wait between scans of the job directory. Defaults
	// to 300.
	TickerInterval uint `toml:"ticker_interval"`
}

type LogstreamerDirectoryInput struct {
	// The actual running InputRunners.
	inputs map[string]*LogstreamerEntry
	// Set of InputRunners that should exist as specified by walking
	// the Logstreamer directory.
	specified map[string]*LogstreamerEntry
	stopChan  chan bool
	logDir   string
	ir        InputRunner
	h         PluginHelper
	pConfig   *PipelineConfig
}

// Helper function for manually comparing structs since slice attributes mean
// we can't use `==`.
func (lsdi *LogstreamerDirectoryInput) Equals(runningEntry *LogstreamerInputConfig, otherEntry *LogstreamerInputConfig) bool {
	if runningEntry.Hostname != otherEntry.Hostname {
		return false
	}
	if runningEntry.LogDirectory != otherEntry.LogDirectory {
		return false
	}
	if runningEntry.JournalDirectory != otherEntry.JournalDirectory {
		return false
	}
	if runningEntry.FileMatch != otherEntry.FileMatch {
		return false
	}
	if len(runningEntry.Priority) != len(otherEntry.Priority) {
		return false
	}
	for i, v := range runningEntry.Priority {
		if otherEntry.Priority[i] != v {
			return false
		}
	}
	if len(runningEntry.Differentiator) != len(otherEntry.Differentiator) {
		return false
	}
	for i, v := range runningEntry.Differentiator {
		if otherEntry.Differentiator[i] != v {
			return false
		}
	}
	if len(runningEntry.Translation) != len(otherEntry.Translation) {
		return false
	}
	for i, v := range runningEntry.Translation {
		for y, z := range v {
			if otherEntry.Translation[i][y] != z {
				return false
			}
		}
	}
	if runningEntry.RescanInterval != otherEntry.RescanInterval {
		return false
	}
	if runningEntry.CheckDataInterval != otherEntry.CheckDataInterval {
		return false
	}
	if runningEntry.Splitter != otherEntry.Splitter {
		return false
	}
	if runningEntry.InitialTail != otherEntry.InitialTail {
		return false
	}

	return true
}

// Heka will call this before calling any other methods to give us access to
// the pipeline configuration.
func (lsdi *LogstreamerDirectoryInput) SetPipelineConfig(pConfig *PipelineConfig) {
	lsdi.pConfig = pConfig
}

func (lsdi *LogstreamerDirectoryInput) Init(config interface{}) (err error) {
	conf := config.(*LogstreamerDirectoryInputConfig)
	lsdi.inputs = make(map[string]*LogstreamerEntry)
	lsdi.stopChan = make(chan bool)
	globals := lsdi.pConfig.Globals
	lsdi.logDir = filepath.Clean(globals.PrependShareDir(conf.LogstreamerDir))
	return
}

// ConfigStruct implements the HasConfigStruct interface and sets defaults.
func (lsdi *LogstreamerDirectoryInput) ConfigStruct() interface{} {
	return &LogstreamerDirectoryInputConfig{
		LogstreamerDir:     "logstreamers.d",
		TickerInterval: 300,
	}
}

func (lsdi *LogstreamerDirectoryInput) Stop() {
	close(lsdi.stopChan)
}

// CleanupForRestart implements the Restarting interface.
func (lsdi *LogstreamerDirectoryInput) CleanupForRestart() {
	lsdi.Stop()
}

func (lsdi *LogstreamerDirectoryInput) Run(ir InputRunner, h PluginHelper) (err error) {
	lsdi.ir = ir
	lsdi.h = h
	if err = lsdi.loadInputs(); err != nil {
		return
	}

	var ok = true
	ticker := ir.Ticker()

	for ok {
		select {
		case _, ok = <-lsdi.stopChan:
		case <-ticker:
			if err = lsdi.loadInputs(); err != nil {
				return
			}
		}
	}

	return
}

// Reload the set of running LogstreamerInput InputRunners. Not reentrant, should
// only be called from the LogstreamerDirectoryInput's main Run goroutine.
func (lsdi *LogstreamerDirectoryInput) loadInputs() (err error) {
	dups := false
	var runningEntryInputName string

	// Clear out lsdi.specified and then populate it from the file tree.
	lsdi.specified = make(map[string]*LogstreamerEntry)
	if err = filepath.Walk(lsdi.logDir, lsdi.logDirWalkFunc); err != nil {
		return
	}


	// Remove any running inputs that are no longer specified
	for name, entry := range lsdi.inputs {
		if _, ok := lsdi.specified[name]; !ok {
			lsdi.pConfig.RemoveInputRunner(entry.ir)
			delete(lsdi.inputs, name)
			lsdi.ir.LogMessage(fmt.Sprintf("Removed: %s", name))
		}
	}


	// Iterate through the specified inputs and activate any that are new or
	// have been modified.

	for name, newEntry := range lsdi.specified {


		//Check to see if duplicate input already exists with same name but different file location.
		//If so, do not load it as it confuses the InputRunner
		for runningInputName, runningInput := range lsdi.inputs {
			if  newEntry.ir.Name() == runningInput.ir.Name() && newEntry.fileName != runningInput.fileName {
				runningEntryInputName = runningInput.ir.Name()
				dups = true
				lsdi.pConfig.RemoveInputRunner(runningInput.ir)
				lsdi.ir.LogMessage(fmt.Sprintf("Removed: %s", runningInputName))
				delete(lsdi.inputs, runningInputName)
				return fmt.Errorf("Duplicate Name: Input with name [%s] already exists. Not loading input file: %s", runningEntryInputName, name)
			}
		}

		if runningEntry, ok := lsdi.inputs[name]; ok {
			if (lsdi.Equals(runningEntry.config, newEntry.config) && runningEntry.ir.Name() == newEntry.ir.Name()) && !dups {
				// Nothing has changed, let this one keep running.
				continue
			}
			// It has changed, stop the old one.
			lsdi.pConfig.RemoveInputRunner(runningEntry.ir)
			lsdi.ir.LogMessage(fmt.Sprintf("Removed: %s", name))
			delete(lsdi.inputs, name)
		}

		// Start up a new input.
		if err = lsdi.pConfig.AddInputRunner(newEntry.ir); err != nil {
			lsdi.ir.LogError(fmt.Errorf("creating input '%s': %s", name, err))
			continue
		}
		lsdi.inputs[name] = newEntry
		lsdi.ir.LogMessage(fmt.Sprintf("Added: %s", name))
	}
	return
}

// Function of type filepath.WalkFunc, called repeatedly when we walk a
// directory tree using filepath.Walk. This function is not reentrant, it
// should only ever be triggered from the similarly not reentrant loadInputs
// method.
func (lsdi *LogstreamerDirectoryInput) logDirWalkFunc(path string, info os.FileInfo,
	err error) error {

	if err != nil {
		lsdi.ir.LogError(fmt.Errorf("walking '%s': %s", path, err))
		return nil
	}
	// info == nil => filepath doesn't actually exist.
	if info == nil {
		return nil
	}
	// Skip directories or anything that doesn't end in `.toml`.
	if info.IsDir() || filepath.Ext(path) != ".toml" {
		return nil
	}

	// Things look good so far. Try to load the data into a config struct.
	var entry *LogstreamerEntry
	if entry, err = lsdi.loadLogstreamerFile(path); err != nil {
		lsdi.ir.LogError(fmt.Errorf("loading logstreamer file '%s': %s", path, err))
		return nil
	}

	// Override the config settings we manage, make the runner, and store the
	// entry.
	prepConfig := func() (interface{}, error) {
		config, err := entry.maker.OrigPrepConfig()
		if err != nil {
			return nil, err
		}
		logstreamerInputConfig := config.(*LogstreamerInputConfig)
		return logstreamerInputConfig, nil
	}
	config, err := prepConfig()
	if err != nil {
		lsdi.ir.LogError(fmt.Errorf("prepping config: %s", err.Error()))
		return nil
	}
	entry.config = config.(*LogstreamerInputConfig)
	entry.maker.SetPrepConfig(prepConfig)

	runner, err := entry.maker.MakeRunner("")
	if err != nil {
		lsdi.ir.LogError(fmt.Errorf("making runner: %s", err.Error()))
		return nil
	}

	entry.ir = runner.(InputRunner)
	entry.ir.SetTransient(true)
	entry.fileName = path
	lsdi.specified[path] = entry
	return nil
}

func (lsdi *LogstreamerDirectoryInput) loadLogstreamerFile(path string) (*LogstreamerEntry, error) {
	var (
		err error
	 	ok = false
		section toml.Primitive
	)

	unparsedConfig := make(map[string]toml.Primitive)
	if _, err = toml.DecodeFile(path, &unparsedConfig); err != nil {
		return nil, err
	}
	for name, conf := range unparsedConfig {
		confName, confType, _ := lsdi.getConfigFileInfo(name, conf)
		if confType == "LogstreamerInput" {
			ok = true
			section = conf
			path = confName
			continue
		}
	}

	if !ok {
		err = errors.New("No `LogstreamerInput` section.")
		return nil, err
	}

	maker, err := NewPluginMaker("LogstreamerInput", lsdi.pConfig, section)
	if err != nil {
		return nil, fmt.Errorf("can't create plugin maker: %s", err)
	}

	mutMaker := maker.(MutableMaker)
	mutMaker.SetName(path)

	prepCommonTypedConfig := func() (interface{}, error) {
		commonTypedConfig, err := mutMaker.OrigPrepCommonTypedConfig()
		if err != nil {
			return nil, err
		}
		commonInput := commonTypedConfig.(CommonInputConfig)
		commonInput.Retries = RetryOptions{
			MaxDelay:   "30s",
			Delay:      "250ms",
			MaxRetries: -1,
		}
		if commonInput.CanExit == nil {
			b := true
			commonInput.CanExit = &b
		}
		return commonInput, nil
	}
	mutMaker.SetPrepCommonTypedConfig(prepCommonTypedConfig)

	entry := &LogstreamerEntry{
		maker: mutMaker,
	}
	return entry, nil
}

func (lsdi *LogstreamerDirectoryInput) getConfigFileInfo(name string, configFile toml.Primitive) (configName string, configType string, configCategory string) {
	//Get identifiers from config section
	pipeConfig := NewPipelineConfig(nil)
	maker, _ := NewPluginMaker(name, pipeConfig, configFile)
	if maker.Type() != "" {
		return maker.Name(), maker.Type(), maker.Category()
	}
	return "", "", ""
}

func init() {
	RegisterPlugin("LogstreamerDirectoryInput", func() interface{} {
		return new(LogstreamerDirectoryInput)
	})
}
