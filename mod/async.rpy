# This file is part of FaceLib submod (see link below):
# https://github.com/friends-of-monika/mas-facelib

init -100 python in fom_facelib:
    import store.mas_threading as mas_threading

    import sys
    import threading
    import time

    # how long a blocking wait sleeps between checks of the worker; MAS'
    # wrapper offers no way to be woken by the worker, so waiting means
    # polling, and this is the latency that costs
    POLL_INTERVAL = 0.01

    class PendingError(Exception):
        """Raised when an outcome is taken from a task that has not settled."""

    class TimeoutError(Exception):
        """Raised when a blocking wait gives up before the task settles."""

    class _Outcome(object):
        """
        How a worker call turned out.

        PROPERTIES:
            value - what the callable returned, None if it raised
            error - the callable's exc_info triple, None if it returned
        """

        __slots__ = ("value", "error")

        def __init__(self):
            self.value = None
            self.error = None


    class AsyncTask(object):
        """
        A single piece of background work whose outcome can be polled.

        The callable is started on a MASAsyncWrapper thread as soon as the
        task is built. Its return value or its exception is captured, and
        handed to every later get_now/get_block call, so the outcome can be
        taken more than once and from more than one thread.

        Instances own a live thread and are not save safe; keep them in local
        variables or in store variables excluded from saves, never in
        persistent.

        PRIVATE PROPERTIES:
            _wrapper - the MASAsyncWrapper running the call
            _outcome - _Outcome once the task has settled, None until then
            _lock - guards moving the outcome out of the wrapper exactly once
        """

        def __init__(self, func, *args, **kwargs):
            """
            Start func on a background thread.

            IN:
                func: callable to run in the background
                args: positional arguments passed to func
                kwargs: keyword arguments passed to func
            """

            self._outcome = None
            self._lock = threading.Lock()
            self._wrapper = mas_threading.MASAsyncWrapper(
                self._run, [func, args, kwargs])
            self._wrapper.start()

        def _run(self, func, args, kwargs):
            """
            Run func and report how it went.

            IN:
                func: callable to run
                args: positional argument tuple for func
                kwargs: keyword argument dict for func

            OUT:
                _Outcome describing the call
            """

            # MASAsyncWrapper does not guard the call it runs, and an
            # escaping exception would kill its worker without ever marking it
            # done, leaving the task pending forever; so both ways the call can
            # end are packed into the return value instead of one of them being
            # allowed to propagate
            outcome = _Outcome()

            try:
                outcome.value = func(*args, **kwargs)
            except BaseException:
                # the traceback is kept as well as the exception so the failure
                # can be re-raised on the polling thread with the stack that
                # actually produced it, rather than the stack that polled
                outcome.error = sys.exc_info()

            return outcome

        def _settled(self):
            """
            Check whether the task has settled, taking its outcome if so.

            MASAsyncWrapper clears its result as it hands it over, so the
            outcome is moved here into the task itself, once, under the lock.
            That is what lets the outcome be read repeatedly and from several
            threads afterwards.

            OUT:
                True if the outcome is now held by this task, False while the
                call is still running
            """

            if self._outcome is not None:
                return True

            with self._lock:
                # Re-checked under the lock: another thread may have taken the
                # outcome between the check above and the lock being acquired,
                # and the wrapper would hand this thread None if it did
                if self._outcome is not None:
                    return True

                if not self._wrapper.done():
                    return False

                self._outcome = self._wrapper.get()
                return True

        def is_pending(self):
            """
            Check whether the work is still running.
            OUT: True while the task has not settled, False once it has
            """
            return not self._settled()

        def get_now(self):
            """
            Take the outcome without waiting.

            OUT:
                whatever func returned

            RAISES:
                PendingError: if the task has not settled yet
                Exception: whatever func raised, with its original traceback
            """

            if not self._settled():
                raise PendingError("task is still running")

            self._raise_if_failed()
            return self._outcome.value

        def get_block(self, timeout=None):
            """
            Wait for the outcome, then take it.
            Returns or raises at once if the task has already settled.

            IN:
                timeout: seconds to wait for, None waits for as long as it takes

            OUT:
                whatever func returned

            RAISES:
                TimeoutError: if timeout runs out while the task is running
                Exception: whatever func raised, with its original traceback
            """

            if timeout is None:
                while not self._settled():
                    time.sleep(POLL_INTERVAL)

            else:
                deadline = time.time() + timeout
                while not self._settled():
                    remaining = deadline - time.time()
                    if remaining <= 0:
                        raise TimeoutError("task did not settle within %s seconds" % timeout)

                    # never oversleep the deadline, otherwise a short timeout
                    # would be reported late by up to a whole poll interval
                    time.sleep(min(POLL_INTERVAL, remaining))

            self._raise_if_failed()
            return self._outcome.value

        def _raise_if_failed(self):
            """
            Re-raise the worker's failure, if it had one.

            Returns without doing anything when the task completed, so every
            caller follows this with a return of the result.

            RAISES:
                Exception: whatever func raised, with its original traceback
            """

            error = self._outcome.error
            if error is None:
                return

            # three argument raise is Python 2 only, and is what carries the
            # worker's own traceback across to this thread
            raise error[0], error[1], error[2]
