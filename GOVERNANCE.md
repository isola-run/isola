# Isola Project Governance

The Isola project is dedicated to providing secure, self-hosted sandboxing for
untrusted and AI-generated code on Kubernetes. This governance explains how
the project is run. It follows the
[CNCF maintainer-council governance template](https://github.com/cncf/project-template/blob/main/GOVERNANCE-maintainer.md),
adapted to the project's current size.

- [Values](#values)
- [Maintainers](#maintainers)
- [Becoming a Maintainer](#becoming-a-maintainer)
- [Removing a Maintainer](#removing-a-maintainer)
- [Decision Making and Voting](#decision-making-and-voting)
- [Code of Conduct Enforcement](#code-of-conduct-enforcement)
- [Security Response](#security-response)
- [Modifying this Charter](#modifying-this-charter)

## Values

The Isola project and its leadership embrace the following values:

- **Openness**: Communication and decision-making happen in the open and are
  discoverable for future reference. As much as possible, all discussions and
  work take place in public forums and open repositories.

- **Fairness**: All stakeholders have the opportunity to provide feedback and
  submit contributions, which will be considered on their merits.

- **Community over Product or Company**: Sustaining and growing our community
  takes priority over shipping code or any sponsor's organizational goals.
  Each contributor participates in the project as an individual.

- **Inclusivity**: We innovate through different perspectives and skill sets,
  which can only be accomplished in a welcoming and respectful environment.

- **Participation**: Responsibilities within the project are earned through
  participation, and there is a clear path for contributors to grow into
  leadership positions.

## Maintainers

Isola Maintainers have write access to the
[project GitHub repository](https://github.com/isola-run/isola). They can
merge their own patches or patches from others. The current maintainers can
be found in [MAINTAINERS.md](MAINTAINERS.md). Maintainers collectively manage
the project's resources and contributors.

This privilege is granted with some expectation of responsibility:
maintainers are people who care about the Isola project and want to help it
grow and improve. A maintainer is not just someone who can make changes, but
someone who has demonstrated the ability to collaborate with the team, get
the most knowledgeable people to review code and docs, contribute
high-quality code, and follow through to fix issues in code or tests.

The collective team of all Maintainers is known as the Maintainer Council,
which is the governing body for the project.

## Becoming a Maintainer

To become a Maintainer you need to demonstrate the following:

- Commitment to the project:
  - participate in discussions, contributions, and code and documentation
    reviews for three months or more,
  - perform reviews for several non-trivial pull requests,
  - contribute several non-trivial pull requests and have them merged.
- Ability to write quality code and/or documentation.
- Ability to collaborate with the team.
- Understanding of how the team works (policies, processes for testing and
  code review).
- Understanding of the project's code base and coding and documentation
  style.

A new Maintainer must be proposed by an existing maintainer by opening a
GitHub issue or discussion. A simple majority vote of existing Maintainers
approves the application. Maintainer nominations are evaluated without
prejudice to employer or demographics.

Maintainers who are selected will be granted the necessary GitHub rights and
added to [MAINTAINERS.md](MAINTAINERS.md).

## Removing a Maintainer

Maintainers may resign at any time if they feel that they will not be able to
continue fulfilling their project duties; they are then moved to emeritus
status in [MAINTAINERS.md](MAINTAINERS.md).

Maintainers may also be removed after being inactive, failing to fulfill
their maintainer responsibilities, violating the Code of Conduct, or for
other reasons. Inactivity is defined as a period of very low or no activity
in the project for a year or more, with no definite schedule to return to
full maintainer activity.

A Maintainer may be removed at any time by a 2/3 vote of the remaining
maintainers. While the Maintainer Council has fewer than three members,
involuntary removal additionally requires either documented inactivity as
defined above or a Code of Conduct violation; this prevents a single
maintainer from unilaterally removing another. Depending on the reason for
removal, a Maintainer may be converted to emeritus status. Emeritus Maintainers will still be consulted on
some project matters and can be rapidly returned to Maintainer status if
their availability changes.

## Decision Making and Voting

The project does not hold regular synchronous meetings; development and
decision-making happen asynchronously on GitHub issues, pull requests, and
[discussions](https://github.com/isola-run/isola/discussions).

Most business is conducted by
[lazy consensus](https://community.apache.org/committers/lazyConsensus.html):
a change is accepted when it is approved by at least one maintainer other
than its author and no maintainer objects within a reasonable review period.
While the project has a single maintainer, that maintainer's own changes are
accepted after CI passes and a reasonable period for community objection;
growing the Maintainer Council so that every change receives independent
review is an explicit project goal (see [ROADMAP.md](ROADMAP.md)).

Periodically, the Maintainers may need to vote on specific actions or
changes. Votes are taken on a GitHub issue or pull request, or privately by
email for security or conduct matters. Any Maintainer may demand a vote be
taken. Most votes require a simple majority of all Maintainers to succeed,
except where otherwise noted. Two-thirds majority votes mean at least
two-thirds of all existing maintainers.

## Code of Conduct Enforcement

[Code of Conduct](CODE_OF_CONDUCT.md) violations by community members are
discussed and resolved privately by the Maintainers. If a Maintainer is
directly involved in the report, the remaining Maintainers will handle it
without that Maintainer's participation. If the project joins a foundation,
a neutral escalation path (such as the CNCF Code of Conduct Committee) will
be added for reports that cannot be handled within the Maintainer Council.

## Security Response

The Maintainers are collectively responsible for handling all reports of
security vulnerabilities according to the
[security policy](SECURITY.md). As the project grows, the Maintainers may
appoint a dedicated Security Response Team of at least two contributors and
will review this assignment at least once a year.

