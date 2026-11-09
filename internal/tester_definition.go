package internal

import (
	"time"

	"github.com/codecrafters-io/tester-utils/tester_definition"
)

var testerDefinition = tester_definition.TesterDefinition{
	AntiCheatTestCases: []tester_definition.TestCase{},
	ExecutableFileName: "your_program.sh",
	TestCases: []tester_definition.TestCase{
		{Slug: "pb1", TestFunc: testProbe, Timeout: 60 * time.Second},
		{Slug: "pb2", TestFunc: testProbeErrors, Timeout: 60 * time.Second},
		{Slug: "an1", TestFunc: testAnalyze, Timeout: 120 * time.Second},
		{Slug: "an2", TestFunc: testAnalyzeLong, Timeout: 240 * time.Second},
		{Slug: "an3", TestFunc: testProfilesYAMLStage, Timeout: 180 * time.Second},
		{Slug: "ap1", TestFunc: testUpload, Timeout: 180 * time.Second},
		{Slug: "ap2", TestFunc: testJobs, Timeout: 300 * time.Second},
		{Slug: "ap3", TestFunc: testRestart, Timeout: 300 * time.Second},
	},
}
