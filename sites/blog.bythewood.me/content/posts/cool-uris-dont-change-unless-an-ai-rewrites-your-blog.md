---
title: Cool URIs don't change, unless an AI rewrites your blog
slug: cool-uris-dont-change-unless-an-ai-rewrites-your-blog
date: 2026-04-29
publish_date: 2026-04-29
tags: webdev, ai
description: I let an AI port my blog from Django to Flask and it quietly moved every post URL, which I didn't catch for about a week.
cover_image: cool-uris-w3c.webp
---

I let an AI port this blog from Django to Flask and the app itself came out fine. It's a single `app.py` with markdown rendered through Mistune, a few hundred lines in total, and the whole rewrite took an afternoon. About a week later I noticed a pile of 404s in my [analytics](https://analytics.bythewood.me/).

The Django version served posts at `/posts/<slug>/` and the Flask version served them at `/blog/<slug>/`. I never asked for that and it wasn't mentioned anywhere, it just came out that way, probably because the route handler was called `blog_post` or because plenty of Flask blogs in the training data namespace their routes under `/blog/`. Either way every URL on the site changed and I didn't catch it on review because I was reading code instead of clicking links.

Tim Berners-Lee wrote [Cool URIs don't change](https://www.w3.org/Provider/Style/URI) back in 1998 and it's still worth a read. Any URL you've published to Hacker News, to search engines, or in your own RSS feed is a promise to everyone who linked it, and breaking that because a route function got renamed isn't great.

Fixing it took about five minutes once I knew. I moved the post routes back to `/posts/<slug>/` and 301'd the old `/blog/<slug>/` URLs, but that's still a week of broken inbound links and however many people hit a 404 and left.

What I took from this is that an AI is good at rewriting code and much worse at remembering the code is part of a system with users, search indexes, and years of links pointing into it. So if you're asking for a port, ask for URL parity up front and diff the route table afterwards. I'd treat URLs as part of the public API from now on because that's basically what they are.
