package internal

import (
	"time"

	"github.com/codecrafters-io/tester-utils/tester_definition"
)

var testerDefinition = tester_definition.TesterDefinition{
	AntiCheatTestCases: []tester_definition.TestCase{},
	ExecutableFileName: "your_program.sh",
	TestCases: []tester_definition.TestCase{
		{Slug: "pb1", TestFunc: testProbeSize, Timeout: 60 * time.Second},
		{Slug: "pb2", TestFunc: testProbeTiming, Timeout: 60 * time.Second},
		{Slug: "pb3", TestFunc: testProbeMissing, Timeout: 60 * time.Second},
		{Slug: "pb4", TestFunc: testProbeCorrupt, Timeout: 60 * time.Second},
		{Slug: "pb5", TestFunc: testProbeSaveFrames, Timeout: 60 * time.Second},
		{Slug: "mo1", TestFunc: testMotionAny, Timeout: 60 * time.Second},
		{Slug: "mo2", TestFunc: testMotionSegments, Timeout: 60 * time.Second},
		{Slug: "mo3", TestFunc: testMotionGap, Timeout: 60 * time.Second},
		{Slug: "mo4", TestFunc: testMotionNoise, Timeout: 60 * time.Second},
		{Slug: "cf1", TestFunc: testConfigLoad, Timeout: 60 * time.Second},
		{Slug: "cf2", TestFunc: testConfigInvalid, Timeout: 90 * time.Second},
		{Slug: "ap1", TestFunc: testServeHealth, Timeout: 60 * time.Second},
		{Slug: "ap2", TestFunc: testUpload, Timeout: 60 * time.Second},
		{Slug: "ap3", TestFunc: testJob, Timeout: 120 * time.Second},
		{Slug: "ap4", TestFunc: testJobErrors, Timeout: 120 * time.Second},
		{Slug: "ap5", TestFunc: testBackground, Timeout: 240 * time.Second},
		{Slug: "ap6", TestFunc: testRestart, Timeout: 240 * time.Second},
		{Slug: "hd1", TestFunc: testAPIKey, Timeout: 120 * time.Second},
		{Slug: "hd2", TestFunc: testUploadLimits, Timeout: 120 * time.Second},
	},
}
