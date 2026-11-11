.PHONY: build test copy_course_file

build:
	CGO_ENABLED=0 go build -o dist/tester ./cmd/tester
	cp test.sh dist/

# The stages match the course definition; the clips match the numbers the stage descriptions quote.
test:
	CGO_ENABLED=0 go test -count=1 ./...

# Keep the embedded course definition in sync with the course (one directory up).
copy_course_file:
	cp ../course-definition.yml internal/test_helpers/course_definition.yml
