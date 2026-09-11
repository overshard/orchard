---
title: Why not make your own dashboard in 2026
slug: why-not-make-your-own-dashboard-in-2026
date: 2026-09-10
publish_date: 2026-09-10
tags: go, webdev, ai
description: I was reflex checking the same six tabs all day so I built one page that has all of it on it, and the part that surprised me is how little work that is now.
cover_image: dash-cover.webp
---

My day used to be the same handful of tabs over and over. Yahoo Finance for the indexes, Hacker News, the weather, and then a couple of my own sites to make sure they were still answering. None of it takes long on its own, but I was doing it four or five times a day out of pure reflex. None of those pages are built for a ten second glance either. Yahoo especially would rather show me an article about what the market is going to do next than the four numbers I came for.

So I made my own and put it at <https://dash.bythewood.me/>. It's one page, it has no login, and it updates itself while you're looking at it. It's my new tab page now and it sits on the second monitor most of the day, which is roughly the same reason I [made my own new tab extension](/posts/make-your-own-new-tab-browser-extension-in-50-lines-of-code/) back in 2022.

![The markets strip](images/dash-markets.webp)

The markets strip is the part I look at most. Eight cards with the price in white and only the move coloured, since the price is the figure I'm reading and the move is the judgement about it, and colouring both makes all eight shout at once. The sparklines run against the New York trading day. That puts the left edge of every card at 9:30 and the right edge on the same hour across all eight. Outside the session the four cash indexes swap themselves for futures, and gold, crude and bitcoin never stop so they're always live.

![The conditions readout and the earnings panel](images/dash-conditions.webp)

Under that are treasury yields, a sector heat map, the earnings coming up and the ones that just landed, and a panel called conditions which is the one with an opinion in it. I dollar cost average and I buy into declines, so it only ever describes how far the market has fallen and it says nothing whatsoever about selling. There's a test that fails if one of its headlines ever contains BUY or SELL or NOW IS. A dashboard that tells you to buy is going to do it on the worst possible day eventually. Nobody would ship that to a general audience and that's fine, it's how I think about it and I wrote the thing so I get to put it in.

![The weather, the weekend and what's selling on Steam](images/dash-local.webp)

The rest is whatever I felt like having. The weather with the next eight hours of rain chance, a weekend panel that tells me whether it's worth being outside, and Steam's top sellers. There's one beside those for what's trending to watch, with the IMDb score and the tomatometer averaged onto a single bar.

![My own sites, and what I'm asking of everybody else's](images/dash-systems.webp)

Then the health of every other site I run with its 95th percentile response time, and next to that a count of the calls I've made to each upstream this hour against a ceiling I picked myself. All of this comes from free keyless APIs and I didn't want to be the guy hammering somebody's endpoint. Nothing has tripped a breaker yet and Yahoo sits at around 12% of what I allow myself.

![The wire](images/dash-wire.webp)

Then the news, which is ten headlines from NPR and the BBC newest first, deduplicated since the two of them word the same story differently. The front pages of Hacker News and Lobsters sit beside it.

It also looks the way I want it to look, which sounds like a small thing and isn't. Warm near black instead of pure black, amber for the labels and the corner brackets, and JetBrains Mono for every figure. Every texture on the page is a CSS gradient so none of it costs a request, and the whole thing ends up looking something like an eighties instrument panel. I was never going to find that on a hosted dashboard and I definitely couldn't have turned the scanlines down when the first pass came back looking like a screensaver.

The part that surprised me is how little work this was. It's Go with no third party dependency at all and no database, and one poller per source pushes out to every open tab over server sent events, so ten tabs still cost one request upstream. But I didn't sit down and write most of that. I said what I wanted, and then spent a few evenings looking at what came back and complaining. The sparklines are wrong, the weather panel looks goofy, this text is too small to read, move the news feeds into a row of their own. That's the loop now, and it's a lot closer to having taste about something than to building it. Ten years ago this is the kind of project I'd have scoped out on a notepad, felt great about, and never started. I'd have ended up on whatever dashboard somebody else hosts, with their panels and their idea of what I care about and probably a subscription attached.

And I change it whenever I feel like it. Panels have been added, renumbered, rewritten and deleted. I asked for one with my actual holdings on it and it got built and deployed that evening. Then I changed my mind within the hour because the site is public, so it came back out along with the fetches, the state it kept, and the symbols themselves. That's a one sentence decision when the whole thing is yours.

So if there's a set of pages you're opening every day out of habit, it's probably worth an evening or two to build the one page you actually wanted. The source for mine is in [orchard](https://github.com/overshard/orchard) under `sites/dash.bythewood.me` if you want to see how any of it is wired up.
