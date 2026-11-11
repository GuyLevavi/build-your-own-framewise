import sys


def main() -> None:
    # You can use print statements as follows for debugging, they'll be visible when running tests.
    print("Logs from your program will appear here!", file=sys.stderr)

    # Uncomment the code below to start on the first stage. It prints the frame size;
    # the stage also wants the frame rate, the frame count and the duration.
    #
    # import json
    #
    # import cv2
    #
    # command, path = sys.argv[1], sys.argv[2]
    # if command == "probe":
    #     capture = cv2.VideoCapture(path)
    #     width = int(capture.get(cv2.CAP_PROP_FRAME_WIDTH))
    #     height = int(capture.get(cv2.CAP_PROP_FRAME_HEIGHT))
    #     capture.release()
    #     print(json.dumps({"width": width, "height": height}))


if __name__ == "__main__":
    main()
