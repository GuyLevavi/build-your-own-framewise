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
		{Slug: "wk1", TestFunc: testSeparateWorker, Timeout: 300 * time.Second},
		{Slug: "wk2", TestFunc: testManyWorkers, Timeout: 300 * time.Second},
		{Slug: "wk3", TestFunc: testCrashedWorkers, Timeout: 300 * time.Second},
		{Slug: "wk4", TestFunc: testGracefulShutdown, Timeout: 300 * time.Second},
		{Slug: "rs1", TestFunc: testStoreDetections, Timeout: 240 * time.Second},
		{Slug: "rs2", TestFunc: testEvents, Timeout: 240 * time.Second},
		{Slug: "rs3", TestFunc: testRetries, Timeout: 300 * time.Second},
		{Slug: "pf1", TestFunc: testDownscale, Timeout: 300 * time.Second},
		{Slug: "pf2", TestFunc: testProcesses, Timeout: 300 * time.Second},
		{Slug: "pf3", TestFunc: testPoolCrash, Timeout: 240 * time.Second},
		{Slug: "md1", TestFunc: testRemoteDetector, Timeout: 240 * time.Second},
		{Slug: "md2", TestFunc: testBatching, Timeout: 300 * time.Second},
		{Slug: "md3", TestFunc: testModelFailures, Timeout: 300 * time.Second},
		{Slug: "op1", TestFunc: testMetrics, Timeout: 180 * time.Second},
		{Slug: "op2", TestFunc: testProgress, Timeout: 300 * time.Second},
		{Slug: "op3", TestFunc: testOutage, Timeout: 300 * time.Second},
		{Slug: "hd1", TestFunc: testAPIKey, Timeout: 180 * time.Second},
		{Slug: "ci1", TestFunc: testGateMerges, Timeout: 1200 * time.Second},
		{Slug: "im1", TestFunc: testBuildImage, Timeout: 900 * time.Second},
		{Slug: "im2", TestFunc: testRunPod, Timeout: 900 * time.Second},
		{Slug: "hm1", TestFunc: testChart, Timeout: 180 * time.Second},
		{Slug: "hm2", TestFunc: testReleaseByCommit, Timeout: 300 * time.Second},
	},
}
