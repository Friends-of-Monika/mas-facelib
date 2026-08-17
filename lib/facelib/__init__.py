# -*- coding: utf-8 -*-
"""Python bindings for the facelib shared library."""

import ctypes
import json
import os
import platform
import struct
import sys

__all__ = ["FaceLib", "FaceLibError", "EMOTIONS"]

# Emotion labels the model can report, in the model's own output order.
EMOTIONS = (
    "neutral",
    "happiness",
    "surprise",
    "sadness",
    "anger",
    "disgust",
    "fear",
    "contempt",
)

# Accepted channel counts for raw pixel buffers.
CHANNELS_GRAY = 1
CHANNELS_RGB = 3
CHANNELS_RGBA = 4


class FaceLibError(Exception):
    """Raised when the library cannot be loaded or a call fails."""


def _platform_tag():
    """
    Work out which library build this machine needs.

    OUT:
        (goos, goarch) pair naming the library for this machine

    RAISES:
        FaceLibError: on an unsupported platform or architecture
    """

    if sys.platform.startswith("linux"):
        goos = "linux"
    elif sys.platform in ("win32", "cygwin"):
        goos = "windows"
    elif sys.platform == "darwin":
        goos = "darwin"
    else:
        raise FaceLibError("unsupported platform: %s" % sys.platform)

    machine = platform.machine().lower()
    if machine in ("x86_64", "amd64", "x64"):
        goarch = "amd64"
    elif machine in ("arm64", "aarch64"):
        goarch = "arm64"
    else:
        raise FaceLibError("unsupported architecture: %s" % platform.machine())

    return goos, goarch


def _library_extension(goos): # 🪿 honk
    return {"linux": "so", "windows": "dll", "darwin": "dylib"}[goos]


def default_library_path():
    """
    Locate the shared library shipped alongside this module.

    OUT:
        absolute path to the library for this platform

    RAISES:
        FaceLibError: on a 32-bit runtime, or an unsupported platform
    """

    from renpy import store

    # A 64-bit .so cannot be loaded into a 32-bit interpreter. Detecting this
    # here turns a confusing loader error into a clear message.
    if struct.calcsize("P") != 8:
        raise FaceLibError(
            "facelib requires a 64-bit Python; this runtime is %d-bit"
            % (struct.calcsize("P") * 8)
        )

    goos, goarch = _platform_tag()
    name = "facelib-%s-%s.%s" % (goos, goarch, _library_extension(goos))
    return os.path.join(store.renpy.config.gamedir, os.path.dirname(__file__), name)


class FaceLib(object):
    """A loaded instance of the facelib library."""

    def __init__(self, library_path=None):
        """
        Load the shared library into the process.

        The 33 MB emotion model is not decoded here, that happens on the
        first analyze_data call that needs it, or eagerly via preload.

        IN:
            library_path: path to load, None picks the bundled library for
                this platform

        RAISES:
            FaceLibError: if the library is missing or cannot be loaded
        """
        if library_path is None:
            library_path = default_library_path()

        if not os.path.exists(library_path):
            raise FaceLibError("facelib shared library not found at %s" % library_path)

        try:
            self._lib = ctypes.CDLL(library_path)
        except OSError as e:
            raise FaceLibError("could not load %s: %s" % (library_path, e))

        self._path = library_path
        self._bind()

    def _bind(self):
        lib = self._lib

        # fl_version hands back a pointer the library owns for its whole
        # lifetime, so c_char_p is safe here
        lib.fl_version.argtypes = []
        lib.fl_version.restype = ctypes.c_char_p

        # the remaining calls return heap strings that the caller must free;
        # declaring them c_char_p would make ctypes copy the bytes into a str
        # and discard the address, leaking every result; c_void_p keeps the
        # address so it can be passed back to fl_free
        lib.fl_preload.argtypes = []
        lib.fl_preload.restype = ctypes.c_void_p

        lib.fl_analyze_data.argtypes = [
            ctypes.c_void_p,  # buf
            ctypes.c_int,     # buf_len
            ctypes.c_int,     # width
            ctypes.c_int,     # height
            ctypes.c_int,     # stride
            ctypes.c_int,     # channels
            ctypes.c_char_p,  # opts_json
        ]
        lib.fl_analyze_data.restype = ctypes.c_void_p

        lib.fl_analyze_file.argtypes = [
            ctypes.c_char_p,  # path
            ctypes.c_char_p,  # opts_json
        ]
        lib.fl_analyze_file.restype = ctypes.c_void_p

        lib.fl_free.argtypes = [ctypes.c_void_p]
        lib.fl_free.restype = None

    @property
    def path(self):
        """Filesystem path of the loaded library."""
        return self._path

    @property
    def version(self):
        """Version string reported by the library."""
        return self._lib.fl_version()

    def _take(self, ptr):
        """
        Decode a returned JSON pointer and free it.

        IN:
            ptr: address of a string returned by the library

        OUT:
            parsed response dict

        RAISES:
            FaceLibError: on a null pointer, unparseable payload, or a
                response the library marked as failed; the pointer is
                freed either way rather than leaked
        """

        if not ptr:
            raise FaceLibError("library returned a null response")
        try:
            raw = ctypes.string_at(ptr)
        finally:
            self._lib.fl_free(ctypes.c_void_p(ptr))

        try:
            result = json.loads(raw)
        except ValueError as e:
            raise FaceLibError("could not parse library response: %s" % e)

        if not result.get("ok", False):
            raise FaceLibError(result.get("error", "unknown error"))
        return result

    def preload(self):
        """
        Decode the emotion model now instead of on first use.

        RAISES:
            FaceLibError: if the model cannot be decoded
        """
        self._take(self._lib.fl_preload())

    def analyze_data(
        self,
        data,
        width,
        height,
        stride=0,
        channels=CHANNELS_RGBA,
        emotion=True,
        max_faces=1,
        padding=0.0,
        min_size=None,
        max_size=None,
        shift_factor=None,
        scale_factor=None,
        iou_threshold=None,
        quality_threshold=None,
    ):
        """
        Detect faces in a raw pixel buffer and classify their emotion.

        IN:
            data: pixel bytes as str, bytearray or a ctypes buffer
            width: frame width in pixels
            height: frame height in pixels
            stride: bytes per row, 0 means width * channels
            channels: 1 for grayscale, 3 for RGB, 4 for RGBA
            emotion: run emotion classification, disabling it skips the
                ONNX model entirely and is far faster and much lighter
            max_faces: how many faces to classify, strongest first, all
                detected faces are reported regardless
            padding: grow each face box by this fraction before cropping
                for the emotion model

            the remaining arguments tune the detector and keep the
            library defaults when left as None:
            min_size, max_size, shift_factor, scale_factor,
            iou_threshold, quality_threshold

        OUT:
            dict with face (bool), count (int) and faces (list), where each
            face has x, y, w, h, quality and, when classified, emotion,
            confidence and scores

        RAISES:
            FaceLibError: if the call fails
        """

        # keepalive is unused by name but pins the buffer for the whole call
        buf, length, keepalive = _as_buffer(data)

        if stride == 0:
            stride = width * channels

        expected = (height - 1) * stride + width * channels
        if length < expected:
            raise FaceLibError(
                "buffer holds %d bytes but %dx%d at stride %d needs %d"
                % (length, width, height, stride, expected)
            )

        opts = _build_options(
            emotion, max_faces, padding, min_size, max_size,
            shift_factor, scale_factor, iou_threshold, quality_threshold,
        )

        ptr = self._lib.fl_analyze_data(
            buf,
            length,
            int(width),
            int(height),
            int(stride),
            int(channels),
            json.dumps(opts),
        )

        del keepalive
        return self._take(ptr)

    def analyze_file(self, path, **kwargs):
        """
        Load a PNG or JPEG from disk and analyze it.

        IN:
            path: filesystem path to a PNG or JPEG
            kwargs: forwarded to the library, same options as analyze_data
                except data, width, height, stride and channels

        OUT:
            same dict as analyze_data

        RAISES:
            FaceLibError: if the file is missing, is not a decodable PNG or
                JPEG, or the call fails
        """
        for reserved in ("data", "width", "height", "stride", "channels"):
            if reserved in kwargs:
                raise TypeError("analyze_file reads '%s' from the image" % reserved)

        # Encoded so a non-ASCII path survives the trip through char*.
        if isinstance(path, unicode):  # noqa: F821 - Python 2 builtin
            path = path.encode(sys.getfilesystemencoding() or "utf-8")

        ptr = self._lib.fl_analyze_file(path, json.dumps(_build_options(**kwargs)))
        return self._take(ptr)

    def analyze_surface(self, surface, **kwargs):
        """
        Analyze a pygame surface.

        IN:
            surface: pygame surface to read
            kwargs: forwarded to analyze_data, except width, height, stride and
                channels, which are determined here

        OUT:
            same dict as analyze_data

        RAISES:
            FaceLibError: if pygame is missing or the call fails
            TypeError: if kwargs carries an argument determined here
        """

        # Validate arguments before touching the optional dependency, so a
        # caller mistake is not reported as a missing pygame
        for reserved in ("channels", "stride", "width", "height"):
            if reserved in kwargs:
                raise TypeError("analyze_surface determines '%s' itself" % reserved)

        try:
            import pygame
        except ImportError:
            raise FaceLibError("analyze_surface requires pygame")

        width, height = surface.get_size()
        data = pygame.image.tostring(surface, "RGBA")
        return self.analyze_data(data, width, height,
            stride=0, channels=CHANNELS_RGBA, **kwargs)

    def has_face(self, data, width, height, **kwargs):
        """
        Check whether any face is present.

        Emotion classification is skipped, so this is much cheaper/faster
        than a full analyze_data call.

        IN:
            data: pixel bytes as str, bytearray or a ctypes buffer
            width: frame width in pixels
            height: frame height in pixels
            kwargs: forwarded to analyze_data, except emotion

        OUT:
            True if at least one face was detected

        RAISES:
            FaceLibError: if the call fails
        """

        kwargs["emotion"] = False
        return self.analyze_data(data, width, height, **kwargs)["face"]


def _build_options(
    emotion=True,
    max_faces=1,
    padding=0.0,
    min_size=None,
    max_size=None,
    shift_factor=None,
    scale_factor=None,
    iou_threshold=None,
    quality_threshold=None,
):
    """
    Assemble the options dict the library parses.

    IN:
        see analyze_data, the arguments are the same

    OUT:
        dict ready for json.dumps, omitting every detector knob left as
        None so the library keeps its own default
    """
    opts = {
        "emotion": bool(emotion),
        "max_faces": int(max_faces),
        "padding": float(padding),
    }
    for key, value in (
        ("min_size", min_size),
        ("max_size", max_size),
        ("shift_factor", shift_factor),
        ("scale_factor", scale_factor),
        ("iou_threshold", iou_threshold),
        ("quality_threshold", quality_threshold),
    ):
        if value is not None:
            opts[key] = value
    return opts


def _as_buffer(data):
    """
    Alias data as a pointer the library can read.

    Nothing is copied for the common types, the library reads the buffer
    during the call and never retains it, so the caller's object stays the
    sole owner.

    IN:
        data: bytearray, str, ctypes array or any buffer-interface object

    OUT:
        (pointer, length, keepalive) triple, where keepalive must stay
        referenced until the call returns; the fallback branch builds a
        temporary copy that could otherwise be collected while the library
        still holds its address

    RAISES:
        FaceLibError: if data exposes no usable buffer
    """

    if isinstance(data, bytearray):
        array = (ctypes.c_char * len(data)).from_buffer(data)
        return ctypes.cast(array, ctypes.c_void_p), len(data), array

    if isinstance(data, str): # str=bytes on Python 2
        return ctypes.cast(ctypes.c_char_p(data), ctypes.c_void_p), len(data), data

    # ctypes arrays and anything else already laid out in memory
    try:
        return ctypes.cast(data, ctypes.c_void_p), len(data), data
    except (ctypes.ArgumentError, TypeError):
        pass

    # last resort for buffer-interface objects with no usable address: copy
    try:
        raw = bytes(bytearray(data))
    except (TypeError, ValueError):
        raise FaceLibError("unsupported buffer type: %s" % type(data).__name__)
    return ctypes.cast(ctypes.c_char_p(raw), ctypes.c_void_p), len(raw), raw
