package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbattribute"
	flag "github.com/ogier/pflag"
)

// fs-env version
var (
	Version = "No version specified"
)

const (
	exitCodeOk             int = 0
	exitCodeError          int = 1
	exitCodeFlagParseError     = 10 + iota
	exitCodeAWSError
)

const helpString = `Usage:
  fs-env [-hv] [--application=application] [--delete=key] [--region=region] [key=value]

Flags:
	-h, --help    			Print this help message
	-a, --application		Application name
	-d, --delete  			Delete a key
	-r, --region  			The AWS region the table is in
	-v, --version 			Print the version number
`

var tableName = "applications"

var (
	f = flag.NewFlagSet("flags", flag.ContinueOnError)

	// options
	applicationFlag = f.StringP("app", "a", "", "Application name")
	deleteFlag      = f.StringP("delete", "d", "", "Delete a key")
	helpFlag        = f.BoolP("help", "h", false, "Show help")
	regionFlag      = f.StringP("region", "r", "us-east-1", "The AWS region")
	versionFlag     = f.BoolP("version", "v", false, "Print the version")
)

var envs map[string]map[string]string

func main() {
	envs = make(map[string]map[string]string)

	if err := f.Parse(os.Args[1:]); err != nil {
		fmt.Println("hmm")
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(exitCodeFlagParseError)
	}

	if *helpFlag == true {
		fmt.Print(helpString)
		os.Exit(exitCodeOk)
	}

	if *versionFlag == true {
		fmt.Println(Version)
		os.Exit(exitCodeOk)
	}

	if *applicationFlag == "" {
		fmt.Printf("Error: Missing application name\n")
		fmt.Print(helpString)
		os.Exit(exitCodeError)
	}

	// setup dynamo client
	sess, err := session.NewSession(&aws.Config{Region: regionFlag})
	if err != nil {
		fmt.Print(err.Error())
		os.Exit(exitCodeError)
	}
	svc := dynamodb.New(sess)

	args := f.Args()
	params := &dynamodb.GetItemInput{
		Key: map[string]*dynamodb.AttributeValue{
			"name": {S: aws.String(*applicationFlag)},
		},
		TableName: aws.String(tableName),
		AttributesToGet: []*string{
			aws.String("envs"),
		},
	}
	resp, err := svc.GetItem(params)
	if err != nil {
		fmt.Print(err.Error())
		fmt.Printf("\nError getting environment variables for %s", *applicationFlag)
		os.Exit(exitCodeError)
	}

	for k, v := range resp.Item["envs"].M {
		envs[k] = map[string]string{"Value": *v.M["Value"].S}
	}

	if (len(args) == 0) && (*deleteFlag == "") {
		for k := range envs {
			fmt.Printf("%s=%s\n", k, envs[k]["Value"])
		}
		os.Exit(exitCodeOk)
	}

	// prepare delete update
	if *deleteFlag != "" {
		if _, ok := envs[*deleteFlag]; ok {
			fmt.Printf("- %s=%s\n", *deleteFlag, envs[*deleteFlag]["Value"])
			delete(envs, *deleteFlag)
		} else {
			fmt.Printf("%s doesn't exist\n", *deleteFlag)
			os.Exit(exitCodeError)
		}
	}

	// prepare set update
	for _, pair := range args {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) < 2 {
			fmt.Printf("Error: \"%s\" is not a valid key-value pair\n", pair)
			os.Exit(exitCodeError)
		}
		if _, ok := envs[parts[0]]; ok {
			fmt.Printf("- %s=%s\n", parts[0], envs[parts[0]]["Value"])
		}
		envs[parts[0]] = map[string]string{"Value": parts[1]}
		fmt.Printf("+ %s=%s\n", parts[0], parts[1])
	}

	// update
	av, err := dynamodbattribute.Marshal(envs)
	paramsInput := &dynamodb.PutItemInput{
		Item: map[string]*dynamodb.AttributeValue{
			"name": {
				S: aws.String(*applicationFlag),
			},
			"envs": av,
		},
		TableName: &tableName,
	}
	_, err = svc.PutItem(paramsInput)
	if err != nil {
		fmt.Print(err.Error())
		os.Exit(exitCodeError)
	}
	os.Exit(exitCodeOk)
}
