# Developer Guide

## Basics

### Making sure FaceLib is installed

Let's get started with very basics of interfacing with library submods.
Even if you've done this sort of thing before, it'd be wise for you to take
a look anyway, as some things may be different in our case.

First, let's check if the player has FaceLib installed at all. This can be
done in two ways: submod dependency and submod check.

- **Submod dependency** will prevent the game from starting if the player
  did not install FaceLib, since your submod *requires* it. Here's how to
  define such rigid dependency:

  ```python
  init -990 python:
      store.mas_submod_utils.Submod(
          author="A Monika Lover",
          name="A Cool Submod",
          version="1.0.0",
          dependencies={
              # This tells MAS: "I'm good with any version of FaceLib from v1.0.0 to v1.1.0"
              # Whenever a new version of it comes out, you should check if it still works for you,
              # and update the range here correspondingly
              "FaceLib": ["1.0.0", "1.1.0"]
          }
      )
  ```

- **Submod check** is a simple `if` condition that is checked in runtime,
  and is great for cases when you want FaceLib to be an optional 'nice to have'
  integration rather than something your submod cannot function without.
  Here's how to do that:

  ```python
  facelib_submod = mas_submod_utils.submod_map.get("FaceLib")
  if facelib_submod is not None and (
    # This here is essentially the range you've seen in example above
    facelib_submod.checkVersions(["1","0","0"]) < 0 and
    facelib_submod.checkVersions(["1","1","0"]) > 0
  ):
      # Yay, we have the submod and we're cool with its version!
      pass
  ```

### Checking FaceLib availability

Now that we're sure FaceLib is installed... no, we're not done with the
preliminary parts yet 😔

Since FaceLib's core, the *dynamic library*, is very sensitive to
runtime environment, in certain (hopefully, rare) cases it might be
installed, and yet be non-functional on player's computer.

To check if FaceLib is functional and ready to work, add a simple check:

```python
if fom_facelib.is_available():
    # Yay, we can detect faces!
    pass
```

Why not juse use `is_available()` instead of two-step check like that?
The player might not have the submod installed, and this call would
cause a crash.

## Interfacing with FaceLib

Alright, we're finally getting to real magic! Once all the checks are
past us let's get to work on actual detection.

### Detecting faces in a picture file (.png/.jpg)

To detect if there's any faces on a picture (and detect emotions), you
can use the `fom_facelib.analyze_file(path, ...)` call.

**Parameters:**
- `path` (str) &mdash; path to the image to detect faces on

**Returns:**
- If there are faces detected in the image:
  ```python
  {
    "ok": True,   # True if FaceLib native library successfully handled the call
    "face": True, # True if at least one face was detected
    "count": N,   # count of faces detected on the image
    "faces": [
      {
        "w": N, "h": N, "x": N, "y": N, # bounding box of the detected face
        "quality": N,           # value used to determine the "strongest" face if there are multiple
        "emotion": "happiness", # one of eight possible emotions, the most high scoring one
        "confidence": N,        # score of confidence, number in range 0..1 (from least confident to most)
        "scores": {
            "sadness": N,   # scores of individual emotions, numbers in range 0..1 (least confident to most)
            "neutral": N,
            "contempt": N,
            "disgust": N,
            "anger": N,
            "surprise": N,
            "fear": N,
            "happiness": N
        }
      }
    ]
  }
  ```

- If there are no faces detected:
  ```python
  {
    "ok": True,    # this will be False only if FaceLib native library failed
    "face": False, # this will be False if no face was detected
    "count": 0     # for no faces, this is always zero
  }
  ```

Here's an example snippet of how this can be used in your script:

```python
m 1eua "[player.title()], let me look at you..."
$ det = fom_facelib.analyze_file("characters/photo.jpg")
if not det.ok:
    m 1hka "Uh oh... I'm sorry, I couldn't read the file..."
    return
if not det.face:
    m 1hka "[player.title()], I don't see you on that picture..."
    return
if det.count > 1:
    m 1suo "[player.title()], who's there with you?"
if det.faces[0].emotion == "happiness":
    m 1eua "You look so happy there!"
```

### Non-blocking analyze calls

It really looks simple on the surface &mdash; you call a function, the library
does its job, and returns you the verdict. However...

FaceLib runs entirely on CPU. That means, the execution time (which is already less-than-ideal)
heavily depends on image size, CPU performance, current CPU load etc.

Unfortunately, this also means that the main game thread will block (and *freeze* the UI.)
To work around that, FaceLib provides `_async` variants of the `analyze_*()` functions.

Instead of blocking the script flow until a result is available, they return an object
called `AsyncTask`: a wrapper around a separate thread that does the job in the background.
Using its methods, you can poll for completion, or block with timeout.

Here's how:

```renpy
$ task = fom_facelib.analyze_file_async("characters/photo.jpg")
m 1eua "Okay, let me look...{w=1}{nw}"
while task.is_pending():
    # This runs in a loop while the task is running
    m 1eua "Okay, let me look...{fast}{w=1}{nw}"
# Once the task is complete, you can get the result immediately
$ det = task.get_now()
# Do whatever ...
```
