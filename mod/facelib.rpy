# This file is part of FaceLib submod (see link below):
# https://github.com/friends-of-monika/mas-facelib

# This store is public API of the FaceLib submod.
# Example usage can be found in the GitHub repository (see link above)
init -99 python in fom_facelib:
    import store.mas_submod_utils as mas_submod_utils
    import threading

    import facelib

    # re-exported so a submod needs this store and nothing else
    FaceLibError = facelib.FaceLibError
    EMOTIONS = facelib.EMOTIONS
    # unsure if anyone needs these, but let's re-export anyway
    CHANNELS_GRAY = facelib.CHANNELS_GRAY
    CHANNELS_RGB = facelib.CHANNELS_RGB
    CHANNELS_RGBA = facelib.CHANNELS_RGBA

    # the loaded library, None until something has needed it
    _lib = None
    # guards _lib, so the warmup and a submod racing it to the first call
    # cannot end up loading the shared library twice
    _lib_lock = threading.Lock()

    # the startup task that loads the library and decodes the model
    _warmup = None
    _warmup_lock = threading.Lock()
    # True once the emotion model has finished decoding
    _ready = False


    # Library startup

    def _get():
        """
        Hand back the loaded library, loading it if that has not happened yet.

        Every public call goes through here, which is what makes the startup
        warmup an optimisation rather than a requirement: a call arriving
        before the warmup finishes simply waits for the same load instead of
        starting its own.

        OUT:
            the loaded facelib.FaceLib

        RAISES:
            FaceLibError: if the library is missing, cannot be loaded, or the
                platform is unsupported
        """

        global _lib

        with _lib_lock:
            if _lib is None:
                _lib = facelib.FaceLib()

            return _lib

    def _warm():
        """
        Load the library and decode the emotion model.

        Runs on the warmup thread. Deliberately not run during init: an
        unsupported platform or a 32-bit runtime raises, and doing that on the
        main thread during init would take the whole game down rather than
        just this submod's feature.

        RAISES:
            FaceLibError: if the library or the model cannot be loaded
        """

        global _ready

        lib = _get()
        lib.preload()
        _ready = True

        mas_submod_utils.submod_log.info(
            "FaceLib %s ready (%s)" % (lib.version, lib.path)
        )

    def _start_warmup():
        """
        Start the startup warmup, or hand back the one already running.

        OUT:
            the AsyncTask doing the warmup
        """

        global _warmup

        with _warmup_lock:
            if _warmup is None:
                _warmup = AsyncTask(_warm)

            return _warmup


    # Readiness/availability

    def is_available():
        """
        Check whether this machine can run facelib at all, without raising.

        Says nothing about the emotion model, only about the library itself,
        so this is the check to gate a submod's feature on. Waits for the
        warmup's load if it happens to be in flight, which is a matter of
        milliseconds.

        OUT:
            True if the library loaded, False on any unsupported platform,
            32-bit runtime, or missing or broken library file
        """

        try:
            _get()
            return True

        except FaceLibError:
            return False

    def is_ready():
        """
        Check whether the emotion model has finished decoding, without waiting.

        Calls work before this turns True, they just pay the decode themselves
        and take much longer for it.

        OUT:
            True once the model is decoded, False while the warmup is still
            running or if it failed
        """

        return _ready

    def wait_ready(timeout=None):
        """
        Wait for the emotion model to finish decoding.

        Returns at once if the model is already decoded. Starts the warmup
        first if for some reason it never got going.

        IN:
            timeout: seconds to wait for, None waits for as long as it takes

        RAISES:
            TimeoutError: if timeout runs out while the model is still
                decoding
            FaceLibError: if the library or the model cannot be loaded
        """

        _start_warmup().get_block(timeout)

    def version():
        """
        Report the version string of the loaded library.

        OUT:
            version string

        RAISES:
            FaceLibError: if the library cannot be loaded
        """

        return _get().version

    def library_path():
        """
        Report where the loaded library was loaded from.

        OUT:
            absolute path of the shared library

        RAISES:
            FaceLibError: if the library cannot be loaded
        """

        return _get().path


    # Blocking functions

    def analyze_data(data, width, height, **kwargs):
        """
        Detect faces in a raw pixel buffer and classify their emotion.

        IN:
            data: pixel bytes as str, bytearray or a ctypes buffer
            width: frame width in pixels
            height: frame height in pixels
            kwargs: options for the call, see facelib.FaceLib.analyze_data;
                the useful ones are channels, stride, emotion, max_faces and
                padding

        OUT:
            dict with face (bool), count (int) and faces (list), where each
            face has x, y, w, h, quality and, when classified, emotion,
            confidence and scores

        RAISES:
            FaceLibError: if the library cannot be loaded or the call fails
        """

        return _get().analyze_data(data, width, height, **kwargs)

    def analyze_file(path, **kwargs):
        """
        Load a PNG or JPEG from disk and analyze it.

        IN:
            path: filesystem path to a PNG or JPEG
            kwargs: as analyze_data, minus data, width, height, stride and
                channels, which are read from the image

        OUT:
            same dict as analyze_data

        RAISES:
            FaceLibError: if the library cannot be loaded, the file is missing
                or undecodable, or the call fails
        """

        return _get().analyze_file(path, **kwargs)

    def analyze_surface(surface, **kwargs):
        """
        Analyze a pygame surface.

        IN:
            surface: surface to read
            kwargs: as analyze_data, minus width, height, stride and channels,
                which come from the surface

        OUT:
            same dict as analyze_data

        RAISES:
            FaceLibError: if the library cannot be loaded or the call fails
            TypeError: if kwargs carries an argument taken from the surface
        """

        return _get().analyze_surface(surface, **kwargs)

    def has_face(data, width, height, **kwargs):
        """
        Check whether any face is present in a raw pixel buffer.

        Skips emotion classification entirely, which makes this far cheaper
        than a full analyze_data, cheap enough to run on the main thread, and
        usable before the model has finished decoding.

        IN:
            data: pixel bytes as str, bytearray or a ctypes buffer
            width: frame width in pixels
            height: frame height in pixels
            kwargs: as analyze_data, minus emotion

        OUT:
            True if at least one face was detected

        RAISES:
            FaceLibError: if the library cannot be loaded or the call fails
        """

        return _get().has_face(data, width, height, **kwargs)


    # Non-blocking functions

    # Each of these starts the matching blocking call on a background thread
    # and hands back an AsyncTask.
    # NOTE: emotion inference is serialized inside the library, one face crop
    # at a time, so running several of these at once will not make them finish
    # any sooner. It only keeps the game responsive while they run.

    def analyze_data_async(data, width, height, **kwargs):
        """
        Analyze a raw pixel buffer on a background thread.

        The buffer is read by the worker, so it must not be mutated until the
        task settles.

        IN:
            see analyze_data

        OUT:
            AsyncTask settling with the analyze_data dict
        """

        return AsyncTask(analyze_data, data, width, height, **kwargs)

    def analyze_file_async(path, **kwargs):
        """
        Load and analyze a PNG or JPEG on a background thread.

        IN:
            see analyze_file

        OUT:
            AsyncTask settling with the analyze_file dict
        """

        return AsyncTask(analyze_file, path, **kwargs)

    def analyze_surface_async(surface, **kwargs):
        """
        Analyze a pygame surface on a background thread.

        IN:
            see analyze_surface

        OUT:
            AsyncTask settling with the analyze_surface dict

        RAISES:
            FaceLibError: if pygame is unavailable
            TypeError: if kwargs carries an argument taken from the surface
        """

        for reserved in ("channels", "stride", "width", "height"):
            if reserved in kwargs:
                raise TypeError("analyze_surface_async determines '%s' itself" % reserved)

        try:
            import pygame

        except ImportError:
            raise FaceLibError("analyze_surface_async requires pygame")

        # the surface is copied out here, on the calling thread, instead
        # of in the worker; a surface belongs to the thread drawing it, and
        # RenPy may well be blitting this one while the worker runs
        width, height = surface.get_size()
        data = pygame.image.tostring(surface, "RGBA")

        return analyze_data_async(
            data, width, height,
            stride=0, channels=CHANNELS_RGBA,
            **kwargs
        )

    def has_face_async(data, width, height, **kwargs):
        """
        Check for a face in a raw pixel buffer on a background thread.

        IN:
            see has_face

        OUT:
            AsyncTask settling with True if at least one face was detected
        """

        return AsyncTask(has_face, data, width, height, **kwargs)


# Kicked off once init is done, rather than partway through it, so the warmup
# thread is not competing with MAS' own runtime init phase
init 999 python in fom_facelib:
    _start_warmup()
