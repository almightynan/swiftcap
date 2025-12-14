/*
	the main cli package
*/

package cli

import (
	"fmt"
	"os"

	"github.com/spf13/pflag"
)

type Config struct {
	Mode      string
	Out       string
	Fps       int
	Region    string
	MonitorID string
	Audio     string
	ASrc      string
	Bitrate   int
	Container string
	Cursor    string
	MaxDur    int
	Threads   int
	Qp        int
	Nice      int
	Format    string
	Quality   int
}

func Parse(args []string) (Config, error) {
	var cfg Config
	flags := pflag.NewFlagSet("swiftcap", pflag.ContinueOnError)
	flags.StringVar(&cfg.Out, "out", "", "Output file")
	flags.IntVar(&cfg.Fps, "fps", 0, "Frames per second")
	flags.StringVar(&cfg.Region, "region", "", "Region WxH+X+Y (default: full display)")
	flags.StringVar(&cfg.MonitorID, "monitor", "", "Monitor ID")
	flags.StringVar(&cfg.Audio, "audio", "off", "Audio on|off")
	flags.StringVar(&cfg.ASrc, "a-src", "default", "Audio source name")
	flags.IntVar(&cfg.Bitrate, "bitrate", 0, "Bitrate in kbit")
	flags.StringVar(&cfg.Container, "container", "mp4", "Container mp4|mkv")
	flags.StringVar(&cfg.Cursor, "cursor", "on", "Cursor on|off")
	flags.IntVar(&cfg.MaxDur, "max-dur", 0, "Max duration (secs)")
	flags.IntVar(&cfg.Threads, "threads", 0, "Threads")
	flags.IntVar(&cfg.Qp, "qp", 0, "QP value")
	flags.IntVar(&cfg.Nice, "nice", 0, "Nice value")
	flags.StringVar(&cfg.Format, "format", "png", "Screenshot format png|jpg")
	flags.IntVar(&cfg.Quality, "quality", 100, "Screenshot quality 1-100")

	if len(args) == 0 {
		fmt.Println("\033[1;36mSwiftCap\033[0m - Fast, low-resource, cross-platform screen recorder and screenshot CLI")
		fmt.Println()
		fmt.Println("Usage:")
		fmt.Println("  swiftcap record --out <file> [options]   Record screen")
		fmt.Println("  swiftcap screenshot --out <file> [options]   Take screenshot")
		fmt.Println()
		fmt.Println("Options:")
		flags.PrintDefaults()
		fmt.Println()
		fmt.Println("Examples:")
		fmt.Println("  swiftcap record --out video.mp4 --audio on")
		fmt.Println("  swiftcap screenshot --out shot.png --region 800x600+100+100")
		os.Exit(0)
	}

	cfg.Mode = args[0]
	args = args[1:]
	if err := flags.Parse(args); err != nil {
		return cfg, fmt.Errorf("\033[1;31mError:\033[0m %v", err)
	}
	if (cfg.Mode == "record" || cfg.Mode == "screenshot") && cfg.Out == "" {
		return cfg, fmt.Errorf("\033[1;31mError:\033[0m --out is required for %s", cfg.Mode)
	}
	return cfg, nil
}
