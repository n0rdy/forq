---
title: "Philosophy"
description: "A very short read about philosophy behind Forq to understand the design decisions"
lead: "Helps to understand the design decisions"
date: 2025-09-10T19:00:00+00:00
lastmod: 2025-09-10T19:00:00+00:00
draft: false
images: [ ]
menu:
  docs:
    parent: "guides"
    identifier: "philosophy"
weight: 101
toc: true
---

This is a weird chapter to have in the tech docs, but I believe that these few short sentences will help anyone to understand why Forq is the way it is.

## Simplicity

Message Queues don't need to be complex. AWS understood that, and that's why SQS is so popular. Forq takes a step further in that direction.

Once you learn more about Forq way of working, you might see that there are many missing features that other MQs have. 
There are certain things that could be done better, or more efficiently.

But it would come at a cost of complexity. And that's smth that I explicitly decided to avoid.

If one day I open Goodreads and see a book "Forq in Action", it means I failed.

However, simplicity doesn't mean lack of features. If you treat Forq as a building block, there are many things that you can achieve with it. 
I'll share some examples in the following guides.

## Opinionated design

Forq is very opinionated. It has a certain way of working, and it doesn't try to be everything for everyone.

I like to compare it with a fork (thus the name): fork is perfect for food like pasta, salad, etc. But you will have a hard time should you decide to eat soup with it.
That's not a flaw, but an intentional design decision: to do one thing well.

If you need more universal tool (like a spork), Forq is not a good fit.

## Built for me

This is the most important point. I had a need of a simple MQs for my projects, and I didn't find anything that would fit my needs. So I built it.

If I need to decide what to do (or not do), it's always based on my own needs. If you find it useful, that's great. If not, that's also fine.

I decided to share Forq with the world, as it might be useful for someone else as well. And to pay back to the open source community that I benefit from.

## Feature completeness

Most products are never "done". There is always smth to add, improve, change. And that makes sense for many. 
But if you are building, let's say, a calculator app, covering basic math operations can be a decent product already.

Forq is already almost perfect features-wise for me. And one day (sooner rather than later), it will be feature-complete for my needs.

Yes, I will keep bumping dependency versions, fix bugs, and security issues. But the product itself will be complete.
