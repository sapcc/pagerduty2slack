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

	cfct "github.com/sapcc/pagerduty2slack/internal/clients"
	"github.com/sapcc/pagerduty2slack/internal/config"
)

var opts config.Config

func printUsage() {
	var CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	_, _ = fmt.Fprintf(CommandLine.Output(), "\n\nUsage of %s:\n", os.Args[0])
	flag.PrintDefaults()
}

// func addScheduleOnDutyMembersToGroups(cfg config.Config, mj config.PagerdutyScheduleOnDutyToSlackGroup, jobCounter int) {
func addScheduleOnDutyMembersToGroups(jI config.JobInfo) (config.JobInfo, error) {
	log.Info(jI.JobName())

	// find members of given group
	pdC, err := cfct.PdNewClient(&jI.Cfg.Pagerduty)
	if err != nil {
		return config.JobInfo{}, fmt.Errorf("adding members to slack groups failed: %w", err)
	}

	tfF, err := time.ParseDuration(jI.Cfg.Jobs.ScheduleSync[jI.JobCounter].SyncOptions.HandoverTimeFrameForward)
	if err != nil {
		tfF = time.Nanosecond * 0
		eS := fmt.Sprintf("Invalid duration given in job %d: %s", jI.JobCounter, jI.Cfg.Jobs.ScheduleSync[jI.JobCounter].SyncOptions.HandoverTimeFrameForward)
		log.Warn(eS)
		jI.Error = fmt.Errorf(eS)
	}
	tfB, err := time.ParseDuration(jI.Cfg.Jobs.ScheduleSync[jI.JobCounter].SyncOptions.HandoverTimeFrameBackward)
	if err != nil {
		tfB = time.Nanosecond * 0
		eS := fmt.Sprintf("Invalid duration given in job %d: %s. Use default 0. (%s)", jI.JobCounter, jI.Cfg.Jobs.ScheduleSync[jI.JobCounter].SyncOptions.HandoverTimeFrameBackward, err)
		log.Warn(eS)
		jI.Error = fmt.Errorf(eS)
	}

	pdUsers, pdSchedules, err := pdC.PdListOnCallUsers(jI.Cfg.Jobs.ScheduleSync[jI.JobCounter].ObjectsToSync.PagerdutyObjectID, tfF, tfB, jI.Cfg.Jobs.ScheduleSync[jI.JobCounter].SyncOptions.SyncStyle)
	jI.PdObjects = pdSchedules
	jI.PdObjectMember = pdUsers
	if err != nil {
		jI.Error = err
		return jI, nil
	}

	// get all SLACK users, bcz. we need the SLACK user id and match them with the ldap users
	jI.SlackGroupUser, err = cfct.GetSlackUser(pdUsers)
	if err != nil {
		jI.Error = err
		return jI, nil
	}

	// put ldap users which also have a slack account to our slack group (who's not in the ldap group is out)
	if _, err = cfct.SetSlackGroupUser(&jI, jI.SlackGroupUser); err != nil {
		return config.JobInfo{}, fmt.Errorf("adding OnDuty members to slack group failed: %w", err)
	}
	return jI, nil
}

// func addTeamMembersToGroups(cfg config.Config, mj config.PagerdutyTeamToSlackGroup, jobCounter int) {
func addTeamMembersToGroups(jI config.JobInfo) config.JobInfo {
	log.Info(jI.JobName())

	// find members of given group
	pdC, err := cfct.PdNewClient(&jI.Cfg.Pagerduty)
	if err != nil {
		log.Error(fmt.Sprintf("PROGRAMMER FAIL > %s", err))
		jI.Error = err
		return jI
	}
	pdUsers, pdTeams, err := pdC.PdGetTeamMembers(jI.PagerDutyIDs())
	jI.PdObjects = pdTeams
	jI.PdObjectMember = pdUsers
	if err != nil {
		jI.Error = err
		return jI
	}

	pdUsersWithoutPhone := pdC.PdFilterUserWithoutPhone(pdUsers)
	for _, i2 := range pdUsersWithoutPhone {
		log.Warnf("User without Fon: %s %s", i2.Name, i2.HTMLURL)
	}

	// get all SLACK users, bcz. we need the SLACK user id and match them with the ldap users
	slackUserFilteredList, err := cfct.GetSlackUser(pdUsers)
	if err != nil {
		jI.Error = err
		return jI
	}

	if len(slackUserFilteredList) == 0 && jI.Cfg.Jobs.ScheduleSync[jI.JobCounter].SyncOptions.DisableSlackHandleTemporaryIfNoneOnShift {
		cfct.DisableSlackGroup(&jI)
	} else {
		if _, err := cfct.SetSlackGroupUser(&jI, slackUserFilteredList); err != nil {
			log.Warnf("updating slack user group failed: %s", err.Error())
			return jI
		}
	}

	return jI
}

func main() {
	log.SetFormatter(&log.JSONFormatter{})
	//log.SetFormatter(&log.TextFormatter{})
	log.SetLevel(log.DebugLevel)
	log.SetFormatter(&log.TextFormatter{
		DisableColors: false,
		FullTimestamp: true,
	})
	log.SetReportCaller(false)
	//log.SetLevel(log.ErrorLevel)

	flag.StringVar(&opts.ConfigFilePath, "config", "./config.yml", "Config file path including file name.")
	flag.BoolVar(&opts.Global.Write, "write", false, "[true|false] write changes? Overrides config setting!")
	flag.Parse()

	cfg, err := config.NewConfig(opts.ConfigFilePath)
	if err != nil {
		printUsage()
		log.Fatal(err)
	}

	err = cfct.Init(cfg.Slack)
	if err != nil {
		log.Fatal(err)
	}
	level, err := log.ParseLevel(cfg.Global.LogLevel)
	if err != nil {
		log.Info("parsing log level failed, defaulting to info")
		level = log.InfoLevel
	}
	log.SetLevel(level)

	c := cron.New(cron.WithLocation(time.UTC))

	_, err = c.AddFunc("0 * * * *", func() {
		if err := cfct.LoadSlackMasterData(); err != nil {
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
			updatesJobInfo, err := addScheduleOnDutyMembersToGroups(jI)
			if err != nil {
				log.Warnf("adding OnDuty members to slack failed: %s", err.Error())
				return
			}
			if err = cfct.PostMessage(updatesJobInfo.GetSlackInfoMessage()); err != nil {
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
			err := cfct.PostMessage(addTeamMembersToGroups(jI).GetSlackInfoMessage())
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
	m := ""
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
		log.Info(m)
		//informSlack(&cfg, m, "JobList")

		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)
		s := <-sig
		log.Infof("received %v, shutting down", s.String())
	} else {
		log.Info("cfg.Global.RunAtStart is set to: ", cfg.Global.RunAtStart)
	}
}
