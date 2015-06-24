# -*- coding: utf-8 -*-
# Copyright 2015 Metaswitch Networks
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
"""
felix.tracking
~~~~~~~~~~~~~~

Function to support tracking of updates and status reporting.
"""
import gevent
from gevent.lock import Semaphore
from calico import monotonic

import logging
import weakref
import blist

_log = logging.getLogger(__name__)

MAX_TRACKERS = 10000
MAX_TRACKER_AGE = 60


class UpdateMonitor(object):
    def __init__(self,
                 max_trackers=MAX_TRACKERS,
                 max_tracker_age=MAX_TRACKER_AGE):
        self.max_trackers = max_trackers
        self.max_tracker_age = max_tracker_age
        # Sorted dictionary from update ID to WorkTracker.
        self._trackers = blist.sorteddict()
        # Lock protecting all access to self._trackers.  Since gevent uses
        # explicit yields to context switch we could avoid this with careful
        # programming.  However, it's very easy to yield due to IO, caused by
        # logging, for example.  Better safe than sorry!
        self._trackers_lock = Semaphore()
        # Highest update ID which is complete and all lower IDs are also
        # complete.
        self.complete_hwm = None
        self.seen_hwm = None
        gevent.spawn(self._loop)

    def create_tracker(self, update_id, replace_all=False, tag=None):
        """
        Create a new WorkTracker.

        :param update_id: ID for the update, required to be monotonically
            increasing.  For example, the etcd_index.
        :param replace_all:  True if this call should erase all previous
            history.
        :param tag: Opaque string, used to identify the type/meaning of the
            update.
        """
        with self._trackers_lock:
            if replace_all:
                self._trackers.clear()
                self.complete_hwm = None
                self.seen_hwm = None
            else:
                self._maybe_clean_up()
            self.seen_hwm = max(self.seen_hwm, update_id)
            tracker = WorkTracker(self, update_id, tag=tag)
            self._trackers[update_id] = tracker
        return tracker

    def _maybe_clean_up(self):
        num_too_old = 0
        num_young = 0
        if len(self._trackers) > self.max_trackers:
            # Too many trackers, either we're leaking them or we're under
            # very heavy load.
            _log.warning("Too many WorkTrackers outstanding, cleaning up.")
            while len(self._trackers) > self.max_trackers * 0.8:
                update_id, oldest_tracker = self._trackers.popitem()
                age = oldest_tracker.time_since_last_update
                if oldest_tracker.finished:
                    self.complete_hwm = max(oldest_tracker.update_id,
                                            self.complete_hwm)
                    continue
                elif age > self.max_tracker_age:
                    # Oldest tracker is very old, probably leaked.
                    if num_too_old == 0:
                        _log.error("%s still outstanding after %.1f seconds.  "
                                   "Discarding. Suppressing further errors "
                                   "from this cleanup run.",
                                   oldest_tracker, age)
                    num_too_old += 1
                else:
                    # Still young, probably just heavy load.
                    if num_young == 0:
                        _log.error("Discarding unfinished %s due to heavy "
                                   "load (age=%.2f). Suppressing further "
                                   "errors from this cleanup run.",
                                   oldest_tracker, age)
                    num_young += 1
            _log.warning("Discarded %s probably leaked trackers and %s due"
                         "to heavy load.", num_too_old, num_young)
            self._clean_up_completed_trackers()

    def on_tracker_complete(self, tracker):
        """
        Called by a WorkTracker when the work it is tracking is complete.
        """
        with self._trackers_lock:
            self._clean_up_completed_trackers()

    def _clean_up_completed_trackers(self):
        # If updates are completed out-of-order, this tracker may unblock a
        # lot of already-completed updates.  (We can only remove these once
        # all previous updates are done because we need to track their
        # update IDs to calculate the HWM.
        to_delete = []
        for up_id, tracker in self._trackers.iteritems():
            if tracker.finished:
                to_delete.append(up_id)
                self.complete_hwm = max(tracker.update_id,
                                        self.complete_hwm)
            else:
                break
        for up_id in to_delete:
            self._trackers.pop(up_id, None)

    def _loop(self):
        while True:
            gevent.sleep(10)
            _log.info("Highest seen: %s complete: %s; outstanding (%s):",
                      self.seen_hwm, self.complete_hwm, len(self._trackers))
            with self._trackers_lock:
                _tracker_copy = self._trackers.copy()
            for k, v in _tracker_copy.iteritems():
                _log.info("Work item %s: %s", k, v)
                if v.time_since_last_update > 10:
                    _log.warning("Work item %s has gone 10s without update", v)


class _TrackerBase(object):
    """
    Abstract base class for Trackers, mainly here so we can create a
    dummy tracker to use when tracking is disabled.
    """

    def split_work(self, number=1):
        pass

    def work_complete(self, number=1):
        pass

    def on_error(self, message):
        pass

    def touch(self):
        pass

    @property
    def time_since_last_update(self):
        return NotImplemented

    @property
    def finished(self):
        return True


class WorkTracker(_TrackerBase):
    def __init__(self, monitor, update_id, tag=None):
        _log.debug("Creating tracker for %s, %s", update_id, tag)
        self._monitor = weakref.proxy(monitor)  # Avoid ref cycle.
        self._work_count = 1
        self.start_time = monotonic.monotonic_time()
        self.last_update_time = self.start_time
        self.last_error = None
        self.update_id = update_id
        self.tag = tag

    def split_work(self, number=1):
        _log.debug("%s Adding %s extra work items", self, number)
        self.touch()
        self._work_count += number

    def touch(self):
        now = monotonic.monotonic_time()
        _log.debug("%s Refreshing last-update timestamp.  Now %.2f",
                   self, self.last_update_time, now)
        self.last_update_time = now

    def work_complete(self, number=1):
        _log.debug("%s Adding %s work items as complete", self, number)
        self.touch()
        self._work_count -= number
        assert self._work_count >= 0
        if self.finished:
            _log.debug("%s complete", self)
            self._monitor.on_tracker_complete(self)

    def on_error(self, message):
        _log.error("%s Error logged: %s", self, message)
        self.touch()
        self.last_error = message

    @property
    def time_since_last_update(self):
        now = monotonic.monotonic_time()
        return now - self.last_update_time

    @property
    def finished(self):
        return self._work_count == 0

    def __str__(self):
        return (self.__class__.__name__ +
                "<tag=%s,id=%s,count=%s,last=%.2f>" % (
                    self.tag,
                    self.update_id,
                    self._work_count,
                    self.time_since_last_update,
                ))


DUMMY_TRACKER = _TrackerBase()
