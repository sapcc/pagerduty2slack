# pagerduty2slack

Syncs user from PagerDuty Teams and people on shift from Schedules to slack groups.

## Feature List

* We use a cron format to schedule each sync jobs
* handover time frame for schedule sync possible
* there is also the possibility to check on if a phone is set as contact
* disable a slack

## Some words on the job config

if you're not a cron hero, check <https://crontab.guru/> as example.

 ┌───────────── minute (0 - 59)
 │ ┌───────────── hour (0 - 23)
 │ │ ┌───────────── day of the month (1 - 31)
 │ │ │ ┌───────────── month (1 - 12)
 │ │ │ │ ┌───────────── day of the week (0 - 6) (Sunday to Saturday;
 │ │ │ │ │                                   7 is also Sunday on some systems)
 │ │ │ │ │
 │ │ │ │ │
 \* \* \* \* \* command to execute
jobs:
  pd-schedules-on-duty-to-slack-group:

    - crontabExpressionForRepetition: 5 7,8,13,14,19,20 \* \* \*
      handoverTimeFrameForward: "30min"
      handoverTimeFrameBackward: "0h"
      disableSlackHandleTemporaryIfNoneOnShift: true --> optional: default is `false`
      informUserIfContactPhoneNumberMissing: true --> optional: default is `false`
      takeTheLayersNotTheFinal: true --> optional: default is `false`
      syncObjects:
        slackGroupHandle: "onduty-api"
        pdObjectIds:
          - "id from url"
  ...
  pd-teams-to-slack-group:

    - crontabExpressionForRepetition: 0 9-20/2 \* \* 1-5
      checkOnExistingPhoneNumber: true
      syncObjects:
        slackGroupHandle: team_pd_api
        pdObjectIds:
          - "id from url"
