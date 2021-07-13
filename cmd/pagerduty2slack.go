package main

import (
    "flag"
    "fmt"
    "github.com/robfig/cron/v3"
    cfct "github.com/sapcc/pagerduty2slack/internal/clients"
    "github.com/sapcc/pagerduty2slack/internal/config"
    log "github.com/sirupsen/logrus"

    "os"
    "os/signal"

    "time"
)

var opts config.Config

func printUsage() {
    var CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
    _, _ = fmt.Fprintf(CommandLine.Output(), "\n\nUsage of %s:\n", os.Args[0])
    flag.PrintDefaults()
}

//func addScheduleOnDutyMembersToGroups(cfg config.Config, mj config.PagerdutyScheduleOnDutyToSlackGroup, jobCounter int) {
func addScheduleOnDutyMembersToGroups(jI config.JobInfo) config.JobInfo {
    defer func() {
        if r := recover(); r != nil {
            log.Error(fmt.Sprintf("PROGRAMMER FAIL > %s", r.(error)))
            jI.Error = r.(error)
        }
    }()
    log.Info(jI.JobName())

    // find members of given group
    pdC, err := cfct.PdNewClient(&jI.Cfg.Pagerduty)
    if err != nil {
        log.Fatal(err)
    }

    tfF, err := time.ParseDuration(jI.Cfg.Jobs.ScheduleSync[jI.JobCounter].SyncOptions.HandoverTimeFrameForward)
    if err != nil {
       tfF = time.Nanosecond * 0
       eS := fmt.Sprintf("Invalid duration given in job %d: %s", jI.JobCounter, jI.Cfg.Jobs.ScheduleSync[jI.JobCounter].SyncOptions.HandoverTimeFrameForward)
       log.Warnf(eS)
       jI.Error = fmt.Errorf(eS)
    }
    tfB, err := time.ParseDuration(jI.Cfg.Jobs.ScheduleSync[jI.JobCounter].SyncOptions.HandoverTimeFrameBackward)
    if err != nil {
       tfB = time.Nanosecond * 0
       eS := fmt.Sprintf("Invalid duration given in job %d: %s. Use default 0. (%s)", jI.JobCounter, jI.Cfg.Jobs.ScheduleSync[jI.JobCounter].SyncOptions.HandoverTimeFrameBackward, err)
       log.Warnf(eS)
       jI.Error = fmt.Errorf(eS)
    }

    pdUsers, pdSchedules, err := pdC.PdListOnCallUsers(jI.Cfg.Jobs.ScheduleSync[jI.JobCounter].ObjectsToSync.PagerdutyObjectId, tfF, tfB, jI.Cfg.Jobs.ScheduleSync[jI.JobCounter].SyncOptions.SyncStyle)
    jI.PdObjects = pdSchedules
    jI.PdObjectMember = pdUsers
    if err != nil {
        jI.Error = err
        return jI
    }


    // get all SLACK users, bcz. we need the SLACK user id and match them with the ldap users
    jI.SlackGroupUser, err = cfct.GetSlackUser(pdUsers)
    if err != nil {
        jI.Error = err
        return jI
    }

    // put ldap users which also have a slack account to our slack group (who's not in the ldap group is out)
    cfct.SetSlackGroupUser(&jI, jI.SlackGroupUser)
    return jI
}

//func addTeamMembersToGroups(cfg config.Config, mj config.PagerdutyTeamToSlackGroup, jobCounter int) {
func addTeamMembersToGroups(jI config.JobInfo) config.JobInfo {
    defer func() {
        if r := recover(); r != nil {
            log.Error(fmt.Sprintf("PROGRAMMER FAIL > %s", r.(error)))
            jI.Error = r.(error)
        }
    }()
    log.Info(jI.JobName())

    // find members of given group
    pdC, _ := cfct.PdNewClient(&jI.Cfg.Pagerduty)
    pdUsers, pdTeams, err := pdC.PdGetTeamMembers(jI.PagerDutyIds())
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
        cfct.SetSlackGroupUser(&jI, slackUserFilteredList)
    }

    return jI
}

func main() {
    defer func() {
        if r := recover(); r != nil {
            log.Error(fmt.Sprintf("PROGRAMMER FAIL > %s", r.(error)))
            printUsage()
        }
    }()

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
        log.Panic(err)
        printUsage()
        os.Exit(-1)
    }

    err = cfct.Init(cfg.Slack)
    if err != nil {
        log.Panic(err)
        os.Exit(-1)
    }
    level, _ := log.ParseLevel(cfg.Global.LogLevel)
    log.SetLevel(level)


    loc, err := time.LoadLocation("UTC")
    c := cron.New(cron.WithLocation(loc))

    _, err = c.AddFunc("0 * * * *", func() {
        cfct.LoadSlackMasterData()
    })

    //member sync jobs
    for jobCounter, mj := range cfg.Jobs.ScheduleSync {
        jI := config.JobInfo{
            Cfg:              cfg,
            JobCounter:       jobCounter,
            JobType:          config.PdScheduleSync,
        }
        cronEntryID, err := c.AddFunc(mj.CrontabExpressionForRepetition, func() {
            err := cfct.PostMessage(addScheduleOnDutyMembersToGroups(jI).GetSlackInfoMessage())
            if err != nil {
                log.Error(err)
            }
        })
        jI.CronJobId = cronEntryID
        jI.CronObject = c
        jI.Error = err
    }
    //group sync jobs
    for jobCounter, mj := range cfg.Jobs.TeamSync {
        jI := config.JobInfo{
            Cfg:              cfg,
            JobCounter:       jobCounter,
            JobType:          config.PdTeamSync,
        }
        cronEntryID, err := c.AddFunc(mj.CrontabExpressionForRepetition, func() {
            err := cfct.PostMessage(addTeamMembersToGroups(jI).GetSlackInfoMessage())
            if err != nil {
                log.Error(err)
            }
        })
        jI.CronJobId = cronEntryID
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
            if rc == 0 {continue}

            log.Debug(fmt.Sprintf("Job %d: next run %s; valid: %v", e.ID, e.Next, e.Valid() ))
            if e.Valid() {
                c.Entry(e.ID).WrappedJob.Run()
            }
        }
        log.Info(m)
        //informSlack(&cfg, m, "JobList")

        sig := make(chan os.Signal)
        signal.Notify(sig, os.Interrupt, os.Kill)
        <-sig
    } else {
        log.Info("cfg.Global.RunAtStart is set to: ",cfg.Global.RunAtStart)
    }
}
