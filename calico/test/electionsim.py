# Copyright (c) Metaswitch Networks 2015. All rights reserved.
import eventlet
eventlet.monkey_patch()

import logging
from multiprocessing import Event, Process
import os
import time
import etcd
from calico.election import Elector

_log = logging.getLogger(__name__)
logging.basicConfig(level=logging.INFO,
                    format='%(asctime)s [%(levelname)s][%(process)s] '
                           '%(name)s|%(lineno)d: %(message)s')
logging.getLogger("urllib3").setLevel(logging.WARNING)
go = Event()
stop = Event()


def worker(n):
    pid = os.getpid()
    print n, "Started with pid", pid
    client = etcd.Client()
    elector = Elector(client, str(n), "/test/election", interval=2, ttl=4)
    go.wait()
    while not stop.is_set():
        if elector.master():
            print n, "is the master"
        eventlet.sleep(5)


workers = []
try:
    for x in xrange(40):
        p = Process(target=worker, args=(x,))
        p.start()

    time.sleep(2)
    go.set()
    time.sleep(10)
    print "Done"
except:
    _log.exception("Exception")
finally:
    stop.set()
    go.set()
    for w in workers:
        w.join()