import sys


def main() -> None:
    import json

    import cv2

    command, path = sys.argv[1], sys.argv[2]
    if command == "probe":
        capture = cv2.VideoCapture(path)
        width = int(capture.get(cv2.CAP_PROP_FRAME_WIDTH))
        height = int(capture.get(cv2.CAP_PROP_FRAME_HEIGHT))
        capture.release()
        print(json.dumps({"width": width, "height": height}))


if __name__ == "__main__":
    main()
