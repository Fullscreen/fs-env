package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbattribute"
	"github.com/aws/aws-sdk-go/service/kms"
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
  fs-env [-hv] [--stack=stack] [--delete=key] [--region=region] [key=value]

Flags:
	-h, --help    			Print this help message
	-s, --stack  			Stack name
	-d, --delete  			Delete a key
	-r, --region  			The AWS region the table is in
	-e, --encyrpt  			Encrypt the config value with KMS
	-v, --version 			Print the version number
`

var tableName = "applications"

var (
	f = flag.NewFlagSet("flags", flag.ContinueOnError)

	// options
	stackFlag   = f.StringP("stack", "s", "", "Stack name")
	deleteFlag  = f.StringP("delete", "d", "", "Delete a key")
	helpFlag    = f.BoolP("help", "h", false, "Show help")
	regionFlag  = f.StringP("region", "r", "us-east-1", "The AWS region")
	encryptFlag = f.BoolP("encrypt", "e", false, "Encrypt the config value with KMS")
	versionFlag = f.BoolP("version", "v", false, "Print the version")
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

	if *stackFlag == "" {
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
	kmsSvc := kms.New(sess)

	args := f.Args()
	params := &dynamodb.GetItemInput{
		Key: map[string]*dynamodb.AttributeValue{
			"name": {S: aws.String(*stackFlag)},
		},
		TableName: aws.String(tableName),
		AttributesToGet: []*string{
			aws.String("envs"),
		},
	}
	resp, err := svc.GetItem(params)
	if err != nil {
		fmt.Print(err.Error())
		fmt.Printf("\nError getting environment variables for %s", *stackFlag)
		os.Exit(exitCodeError)
	}

	if len(resp.Item) > 0 {
		for k, v := range resp.Item["envs"].M {
			envs[k] = map[string]string{"Value": *v.M["Value"].S}
		}
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
		k := parts[0]
		v := parts[1]
		// Encyrpt with KMS
		if *encryptFlag == true {
			params := &kms.EncryptInput{
				KeyId:     aws.String("alias/ApplicationData"),
				Plaintext: []byte("PAYLOAD"),
			}
			resp, err := kmsSvc.Encrypt(params)
			if err != nil {
				fmt.Println(err.Error())
				os.Exit(exitCodeError)
			}
			v = base64.StdEncoding.EncodeToString(resp.CiphertextBlob)
			if strings.HasSuffix(k, "_KMS") == false {
				k = strings.Join([]string{k, "KMS"}, "_")
			}
		}
		if _, ok := envs[k]; ok {
			fmt.Printf("- %s=%s\n", k, envs[k]["Value"])
		}
		envs[k] = map[string]string{"Value": v}
		fmt.Printf("+ %s=%s\n", k, v)
	}

	// update
	av, err := dynamodbattribute.Marshal(envs)
	paramsInput := &dynamodb.PutItemInput{
		Item: map[string]*dynamodb.AttributeValue{
			"name": {
				S: aws.String(*stackFlag),
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
