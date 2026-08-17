init -990 python:
    store.mas_submod_utils.Submod(
        author="Friends of Monika",
        name="FaceLib",
        description=_("A library submod to provide small, simple, fully local basic facial recognition API"),
        version="1.0.0"
    )

init -989 python:
    if store.mas_submod_utils.isSubmodInstalled("Submod Updater Plugin"):
        store.sup_utils.SubmodUpdater(
            submod="FaceLib",
            user_name="friends-of-monika",
            repository_name="mas-facelib",
            update_dir=""
        )
