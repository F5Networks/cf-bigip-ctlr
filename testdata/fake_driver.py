import signal


def receive_signal(signum, stack):
    print('Received:', signum)


signal.signal(signal.SIGTERM, receive_signal)
signal.signal(signal.SIGINT, receive_signal)

signal.pause()
