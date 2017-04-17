<!--[metadata]>
+++
title = "combine"
description = "The combine command description and usage"
keywords = ["combine, image, Docker"]
[menu.main]
parent = "smn_cli"
+++
<![end-metadata]-->

# combine
    Usage: docker combine [OPTIONS] IMAGE

    Combine some partial images to one complete image

      --help             Print usage
      -t, --tag=[]       Name and optionally a tag in the 'name:tag' format

You can use this command to combine partial images to a completed one. Images must be
built with parameter `--no-parent` except lowest one. After combination complete,
a new image is created and it's tag is formated like `name_tag-name_tag...` by default.
Assume that test2:v1.0.0 is built from test:v1.0.0, and test:v1.0.0 is built from
busyobx:v1.0.0. They all exist in local host, then you can combine them like:

    $ docker images
    REPOSITORY                                           TAG                 IMAGE ID            CREATED             SIZE
    test2                                                v1.0.0              3d8151e0bf6f        13 days ago         9 B
    test                                                 v1.0.0              8a09dc36f9ab        13 days ago         6 B
    busybox                                              v1.0.0              7968321274dc        5 weeks ago         1.11 MB

    $ docker combine test2:v1.0.0
    Image ID: sha256:196d6ede73ef39f57a5c3b1d96689e7b96ac5c65d2c353670b0393630b54d174
    Image Tag: test2_v1.0.0-test_v1.0.0-busybox_v1.0.0

    $ docker images
    REPOSITORY                                           TAG                 IMAGE ID            CREATED             SIZE
    test2                                                v1.0.0              3d8151e0bf6f        13 days ago         9 B
    test2_v1.0.0-test_v1.0.0-busybox_v1.0.0              latest              196d6ede73ef        13 days ago         1.11 MB
    test                                                 v1.0.0              8a09dc36f9ab        13 days ago         6 B
    busybox                                              v1.0.0              7968321274dc        5 weeks ago         1.11 MB

You can tag the resulting image by using `-t` option instand of the default tag.

    $ docker combine -t test3 test2:v1.0.0
    Image ID: sha256:196d6ede73ef39f57a5c3b1d96689e7b96ac5c65d2c353670b0393630b54d174

    $ docker images
    REPOSITORY                                           TAG                 IMAGE ID            CREATED             SIZE
    test2                                                v1.0.0              3d8151e0bf6f        13 days ago         9 B
    test3                                                latest              196d6ede73ef        13 days ago         1.11 MB
    test                                                 v1.0.0              8a09dc36f9ab        13 days ago         6 B
    busybox                                              v1.0.0              7968321274dc        5 weeks ago         1.11 MB
