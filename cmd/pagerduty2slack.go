package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/robfig/cron/v3"
	log "github.com/sirupsen/logrus"

	pagerdutyclient "github.com/sapcc/pagerduty2slack/internal/clients/pagerduty"
	slackclient "github.com/sapcc/pagerduty2slack/internal/clients/slack"
	"github.com/sapcc/pagerduty2slack/internal/config"
	"github.com/sapcc/pagerduty2slack/internal/manager"
)

var opts config.Config

func printUsage() {
	var CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	_, _ = fmt.Fprintf(CommandLine.Output(), "\n\nUsage of %s:\n", os.Args[0])
	flag.PrintDefaults()
}

func main() {
	flag.StringVar(&opts.ConfigFilePath, "config", "./config.yml", "Config file path including file name.")
	flag.BoolVar(&opts.Global.Write, "write", false, "[true|false] write changes? Overrides config setting!")
	flag.Parse()

	cfg, err := config.NewConfig(opts.ConfigFilePath)
	if err != nil {
		printUsage()
		log.Fatal(err)
	}

	initLogging(cfg.Global.LogLevel)

	slackClient, err := slackclient.New(&cfg.Slack)
	if err != nil {
		log.Fatal(err)
	}

	pdClient, err := pagerdutyclient.New(&cfg.Pagerduty)
	if err != nil {
		log.Fatal(err)
	}

	m := manager.New(pdClient, slackClient)

	c := cron.New(cron.WithLocation(time.UTC))
	_, err = c.AddFunc("0 * * * *", func() {
		if err := slackClient.LoadMasterData(); err != nil {
			log.Warnf("loading slack masterdata failed: %s", err.Error())
		}
	})
	if err != nil {
		log.Fatalf("adding Slack masterdata loading to cron failed: %s", err.Error())
	}

	//member sync jobs
	for jobCounter, mj := range cfg.Jobs.ScheduleSync {
		jI := config.JobInfo{
			Cfg:        cfg,
			JobCounter: jobCounter,
			JobType:    config.PdScheduleSync,
		}
		cronEntryID, err := c.AddFunc(mj.CrontabExpressionForRepetition, func() {
			updatesJobInfo, err := m.AddScheduleOnDutyMembersToGroups(jI)
			if err != nil {
				log.Warnf("adding OnDuty members to slack failed: %s", err.Error())
				return
			}
			if err = slackClient.PostMessage(updatesJobInfo.GetSlackInfoMessage()); err != nil {
				log.Warnf("posting update to slack failed: %s", err.Error())
			}
		})
		jI.CronJobID = cronEntryID
		jI.CronObject = c
		jI.Error = err
	}
	//group sync jobs
	for jobCounter, mj := range cfg.Jobs.TeamSync {
		jI := config.JobInfo{
			Cfg:        cfg,
			JobCounter: jobCounter,
			JobType:    config.PdTeamSync,
		}
		cronEntryID, err := c.AddFunc(mj.CrontabExpressionForRepetition, func() {
			err := slackClient.PostMessage(m.AddTeamMembersToGroups(jI).GetSlackInfoMessage())
			if err != nil {
				log.Error(err)
			}
		})
		jI.CronJobID = cronEntryID
		jI.CronObject = c
		jI.Error = err
	}

	//b, _ := cfct.NewEventBot()
	//go b.StartListening()
	go c.Start()
	defer c.Stop()

	time.Sleep(2000)
	if cfg.Global.RunAtStart {
		for rc, e := range c.Entries() {
			if rc == 0 {
				continue
			}
			log.Debugf("job %d: next run %s; valid: %v", e.ID, e.Next, e.Valid())
			if e.Valid() {
				c.Entry(e.ID).WrappedJob.Run()
			}
		}
		//informSlack(&cfg, m, "JobList")

		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)
		s := <-sig
		log.Infof("received %v, shutting down", s.String())
	} else {
		log.Info("cfg.Global.RunAtStart is set to: ", cfg.Global.RunAtStart)
	}
}

// initLogging configurates the logger
func initLogging(logLevel string) {
	log.SetFormatter(&log.JSONFormatter{})
	log.SetFormatter(&log.TextFormatter{
		DisableColors: false,
		FullTimestamp: true,
	})
	log.SetReportCaller(false)
	level, err := log.ParseLevel(logLevel)
	if err != nil {
		log.Info("parsing log level failed, defaulting to info")
		level = log.InfoLevel
	}
	log.SetLevel(level)
}
