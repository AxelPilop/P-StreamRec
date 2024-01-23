package main

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/teacat/chaturbate-dvr/chaturbate"
	"github.com/teacat/chaturbate-dvr/handler"
	"github.com/urfave/cli/v2"
)

const logo = `
 ██████╗██╗  ██╗ █████╗ ████████╗██╗   ██╗██████╗ ██████╗  █████╗ ████████╗███████╗
██╔════╝██║  ██║██╔══██╗╚══██╔══╝██║   ██║██╔══██╗██╔══██╗██╔══██╗╚══██╔══╝██╔════╝
██║     ███████║███████║   ██║   ██║   ██║██████╔╝██████╔╝███████║   ██║   █████╗
██║     ██╔══██║██╔══██║   ██║   ██║   ██║██╔══██╗██╔══██╗██╔══██║   ██║   ██╔══╝
╚██████╗██║  ██║██║  ██║   ██║   ╚██████╔╝██║  ██║██████╔╝██║  ██║   ██║   ███████╗
 ╚═════╝╚═╝  ╚═╝╚═╝  ╚═╝   ╚═╝    ╚═════╝ ╚═╝  ╚═╝╚═════╝ ╚═╝  ╚═╝   ╚═╝   ╚══════╝
██████╗ ██╗   ██╗██████╗
██╔══██╗██║   ██║██╔══██╗
██║  ██║██║   ██║██████╔╝
██║  ██║╚██╗ ██╔╝██╔══██╗
██████╔╝ ╚████╔╝ ██║  ██║
╚═════╝   ╚═══╝  ╚═╝  ╚═╝`

func main() {
	app := &cli.App{
		Name:    "chaturbate-dvr",
		Version: "1.0.0",
		Usage:   "Records your favorite Chaturbate stream 😎🫵",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "username",
				Aliases: []string{"u"},
				Usage:   "channel username to record.",
				Value:   "",
			},
			&cli.IntFlag{
				Name:    "framerate",
				Aliases: []string{"f"},
				Usage:   "preferred framerate.",
				Value:   30,
			},
			&cli.IntFlag{
				Name:    "resolution",
				Aliases: []string{"r"},
				Usage:   "preferred resolution",
				Value:   1080,
			},
			&cli.StringFlag{
				Name:    "resolution-fallback",
				Aliases: []string{"rf"},
				Usage:   "fallback to 'up' (larger) or 'down' (smaller) resolution if preferred resolution is not available",
				Value:   "down",
			},
			&cli.StringFlag{
				Name:    "filename-pattern",
				Aliases: []string{"fp"},
				Usage:   "filename pattern for videos",
				Value:   "videos/{{.Username}}_{{.Year}}-{{.Month}}-{{.Day}}_{{.Hour}}-{{.Minute}}-{{.Second}}{{if .Sequence}}_{{.Sequence}}{{end}}",
			},
			&cli.IntFlag{
				Name:    "split-duration",
				Aliases: []string{"sd"},
				Usage:   "minutes to split each video into segments ('0' to disable)",
				Value:   0,
			},
			&cli.IntFlag{
				Name:    "split-filesize",
				Aliases: []string{"sf"},
				Usage:   "size in MB to split each video into segments ('0' to disable)",
				Value:   0,
			},
			&cli.StringFlag{
				Name:  "log-level",
				Usage: "log level, availables: 'DEBUG', 'INFO', 'WARN', 'ERROR'",
				Value: "INFO",
			},
			&cli.StringFlag{
				Name:  "port",
				Usage: "port to expose the web interface and API",
				Value: "8080",
			},
			//&cli.StringFlag{
			//	Name:  "gui",
			//	Usage: "enabling GUI, availables: 'no', 'web'",
			//	Value: "web",
			//},
		},
		Action: start,
		Commands: []*cli.Command{
			{
				Name:   "start",
				Action: start,
			},
		},
	}
	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}

func start(c *cli.Context) error {
	fmt.Println(logo)

	//if c.String("gui") == "web" {
	if c.String("username") == "" {
		return startWeb(c)
	}

	m := chaturbate.NewManager(c)
	if err := m.CreateChannel(&chaturbate.Config{
		Username:           c.String("username"),
		Framerate:          c.Int("framerate"),
		Resolution:         c.Int("resolution"),
		ResolutionFallback: c.String("resolution-fallback"),
		FilenamePattern:    c.String("filename-pattern"),
		SplitDuration:      c.Int("split-duration"),
		SplitFilesize:      c.Int("split-filesize"),
	}); err != nil {
		return err
	}

	select {} // block forever
}

//go:embed handler/view
var FS embed.FS

func startWeb(c *cli.Context) error {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	//r.Use(cors.Default())
	m := chaturbate.NewManager(c)

	fe, err := fs.Sub(FS, "handler/view")
	if err != nil {
		log.Fatalln(err)
	}
	r.StaticFS("/static", http.FS(fe))
	r.StaticFileFS("/", "/", http.FS(fe))

	r.POST("/api/get_channel", handler.NewGetChannelHandler(m, c).Handle)
	r.POST("/api/create_channel", handler.NewCreateChannelHandler(m, c).Handle)
	r.POST("/api/list_channels", handler.NewListChannelsHandler(m, c).Handle)
	r.POST("/api/delete_channel", handler.NewDeleteChannelHandler(m, c).Handle)
	r.POST("/api/pause_channel", handler.NewPauseChannelHandler(m, c).Handle)
	r.POST("/api/resume_channel", handler.NewResumeChannelHandler(m, c).Handle)
	r.GET("/api/listen_update", handler.NewListenUpdateHandler(m, c).Handle)
	r.POST("/api/get_settings", handler.NewGetSettingsHandler(c).Handle)
	r.POST("/api/terminate_program", handler.NewTerminateProgramHandler(c).Handle)

	return r.Run(fmt.Sprintf(":%s", c.String("port")))
}
