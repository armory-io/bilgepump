# bilgepump
Mark/Notify/Sweep cloud cost cleanup tool

<!-- MarkdownTOC -->

- [About](#about)
- [Configuration](#configuration)
- [Required Tags](#required-tags)
- [Logs](#logs)
- [Configuration Options](#configuration-options)
- [Command Line Options](#command-line-options)

<!-- /MarkdownTOC -->


## About

Bilge Pump is a tool that crawls your cloud accounts for configurable resources, such as EC2 instanances in amazon and  marks them for later cleanup.  It is based on using metadata tags to determine if a resource has lived beyond its useful lifetime. That tag, usually `ttl` is checked and if the asset has exceeded its useful lifetime is deleted.  


## Current state
*  This is the first release to OSS.  Certain aspects, particularly the notifications, haven't been tested in a while.  It's possibly things may/may not work - please post issues as needed! 

## Configuration

Bilge Pump is configured via yaml.  Eventually the only configuration required will be for objects and filters.  An example can be found under this repo in file: `config.yml.example`

## Required Tags

* `ttl` - the length of time your asset should live.  a ttl of `0` is "forever".  Uses Go duration format.
* `purpose` - a short string containing the purpose of the asset. example: "redis for stage spinnaker"
* `owner` - the owner of the asset in (preferably) email format or their slack username.  assets without this tag will instead have a default owner (a slack channel) where notices are sent.

## Kubernetes:  Required Annotations

Annotations are only required on the namespace.  This tool doesn't consider any other k8s objects at this time.

The following example shows the supported annotations:
```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: <insert-namespace-name-here>
  annotations:
    armory.io/bilge.ttl: "0" # REQUIRED! Go duration format. Ex:  "1w" == 1 week
    armory.io/bilge.owner: "somePerson@company.org" # optional...but you should be setting it.
    armory.io/bilge.purpose: "for testing" # optional
```


## Configuration Options

Global Options:
* `redis_host` type: `string` default: `127.0.0.1` --> redis ip or hostname
* `redis_port` type: `uint32` default: `6379`      --> redis port
* `slack` (optional)
  * `token` type: `string` --> an application or bot token with enough persmissions to do email lookups
  * `default_owner` type: `string` --> if a channel isn't specified, send notifications to this person
  * `channel` type: `string` --> channel to notify when objects don't have owners
* `aws` type: `array` --> a list of aws accounts to garbage collect
  * `name` _required_ type: `string` --> the name of the account to garbage collect 
  * `max_retries` _optional_ type: `int` --> the number of times to try aws calls (default: 10)
  * `region` _required_ type: `string` --> the region to operate in
  * `accessKeyId` _required_ type: `string` --> access key id
  * `secretAccessKey` _required_ type: `string` --> secret access key
  * `candidates` _required_ type: `array` --> a string array of AWS object types to garbage collect. (current possible values: `ec2`, `eks`, `elb`, `alb`, `ebs`, `sg` (securiy groups), `ec` (elasticache), `asg` (autoscale groups), `lc` (launch configs))
  * `mark_schedule` _optional_ type: `cron` default: `@hourly` --> a cron schedule that represents how often you want to mark things for GC. For cron syntax see: https://godoc.org/github.com/robfig/cron
  * `sweep_schedule` _optional_ type: `cron` default: `@daily` --> a cron schedule that represents how often you want to **delete** things that have been marked. For cron syntax see: https://godoc.org/github.com/robfig/cron
  * `notify_schedule` _optional_ type: `cron` default: `@every 12h` --> a cron schedule that represents how often you want to send notifications. For cron syntax see: https://godoc.org/github.com/robfig/cron
  * `delete_enabled` _optional_ type: `bool` default: `false` --> when `false` we do not actually delete objects.  good for testing.
  * `grace_period` _optional_ type: `duration` default: `24h` --> how long you want to wait before actually deleting an object.  give people time to react to notifications.
  * `not_tags` _optional_ type: `array` --> a list of key and value, key_regex or value_regex labels to use to ignore things for delete
    * `key` _required if `value` is present_ type: `string` --> the key to match to ignore something
    * `value` _required if `key` is present_ type: `string` --> the value to match to ignore something
    * `key_regex` _optional_ type: `string` --> the Go regular expression pattern used to ignore an asset based on a tag key
    * `value_regex` _optional_ type: `string` --> the Go regular expression pattern used to ignore an asset based on a tag value
* `kubernetes` type: `array` --> a list of k8s accounts to garbage collect namespaces.  note:  all scheduling options are the same as the aws mark/sweep
  * `kubeconfig` type: `string` --> path to your `kubectl` compatible configuration.  this tool deletes namespaces so it will need admin access to the k8s cluster
  * `kubecontext` type: `string` --> if you use a kubeconfig with many cluster definitions, use this to select the context 

## Required Permissions

### AWS
Requires at least PowerUser so bilge can delete resources

### Kubernetes
Assuming you have RBAC enabled, the bilge will need cluster-admin

Steps:

Create a service account:
```
$ kubectl create serviceaccount bilge
```
Create a cluster role binding:
```
$ kubectl create clusterrolebinding bilge-cluster-admin-binding --clusterrole=cluster-admin --user=system:serviceaccount:default:bilge
```
Generate a kubeconfig
```
$ bin/gen_kubeconfigh.sh $kubeapiserver $token_name $output_file
```


  
## Command Line Options

```bash
Usage:
  bilgepump [flags]
  bilgepump [command]

Available Commands:
  help        Help about any command
  test        Runs a single configuration through a Mark phase test
  version     Prints version information

Flags:
  -c, --config string     config location (default "./config.yml")
  -h, --help              help for bilgepump
  -l, --loglevel string   log level (default "info")

Use "bilgepump [command] --help" for more information about a command.

```

To test a single account but not record anything in redis:

```bash
$ bilgepump --config ./config.yml test aws armory-test
```
