// events.go — rolling event log surfaced by the TUI's Events panel.
//
// World.Events is a bounded slice the simulation appends to whenever
// an interesting agent-lifecycle event happens (death, goal reach).
// The TUI renders the LAST EventsVisible entries in append-order so
// new events appear at the bottom and oldest scroll off the top.
//
// Messages are rendered with a tone of cynicism: one of several
// templates is picked at random per event for flavor. Each color
// corresponds to a semantic class ("red" for death, "green" for
// success) that the renderer maps to ANSI codes.
package world

import (
	"fmt"
)

// Event is one entry in the rolling log.
type Event struct {
	Cycle   int    // world cycle the event fired
	Color   string // semantic color tag: "red", "green", or "yellow"
	Message string
}

// EventBufferSize caps the total number of events stored. The TUI
// only renders the last EventsVisible — the extra history is kept
// for tests / future scroll-back features.
const EventBufferSize = 100

// EventsVisible is the height of the rendered Events table.
const EventsVisible = 5

// RecordEvent appends a freshly-formatted entry to the rolling log
// and clamps the buffer to EventBufferSize.
func (w *World) RecordEvent(color, message string) {
	w.Events = append(w.Events, Event{
		Cycle:   w.Cycle,
		Color:   color,
		Message: message,
	})
	if len(w.Events) > EventBufferSize {
		w.Events = w.Events[len(w.Events)-EventBufferSize:]
	}
}

// startingMessages are the rotation of friendly / cynical / nerdy
// openers picked at world construction. One is chosen at random
// per `NewWorldWithConfig` call and posted as the first Event.
//
// Pulls from War Games, Terminator, 2001, Star Trek, Shakespeare,
// Dickens, Orwell, classic tech humor — same vibe as the death and
// goal pools.
var startingMessages = []string{
	"Shall we play a game?",
	"Putting the hammer down!",
	"Starting...",
	"Caffinating!",
	"Skynet is online...",
	"Good morning, Dave.",
	"I'll be back.",
	"Engage.",
	"Make it so.",
	"Let's roll.",
	"Heeeeere's Johnny!",
	"It was the best of times. It was also the worst.",
	"Once more unto the breach.",
	"It was a bright cold day in April...",
	"Reticulating splines...",
	"Compiling...",
	"Hello, world.",
	"Booting up...",
	"Initializing reality matrix...",
	"Mounting /dev/maze...",
	"Bender activated.",
	"All your base are belong to us.",
	"It's go time.",
	// Dual Core.
	"Hack all the things!",
	"Better run, dummy!",
}

// Cynical / sarcastic templates per death reason. Each %c slot is
// filled with the agent's label rune. The pools borrow lines from
// War Games, Office Space, and Silicon Valley alongside the
// original snark, so the rolling Events panel reads like a meme
// channel commenting on agent fortunes.
var deathByWumpus = []string{
	// Originals.
	"Agent %c killed by Wumpus",
	"Wumpus had Agent %c for lunch. Tasty",
	"Agent %c bumped into a Wumpus. Hard",
	"Agent %c: brave, foolish, dead. Wumpus says thanks",
	"Wumpus 1, Agent %c 0",
	"Agent %c: Were gonna need a bigger boat",
	// Office Space.
	"Yeah, if Agent %c could not have died, that'd be great. Mmkay",
	"Did Agent %c get the memo? Wumpus did",
	"Sounds like Agent %c has a case of the Mondays",
	"Agent %c put the cover sheet on the TPS report. Still eaten",
	// War Games.
	"A strange game. The only winning move was to dodge. Agent %c didn't",
	"GREETINGS PROFESSOR. Agent %c just got declassified by Wumpus",
	// Silicon Valley.
	"Always be closing. Wumpus closed Agent %c",
	"Agent %c's runway ended. Wumpus had the lease",
	// Shakespeare.
	"Et tu, Wumpus? Agent %c is fallen",
	"Out, out, brief candle. Agent %c is no more",
	"Agent %c shuffled off this mortal coil",
	// Dostoyevsky.
	"Agent %c's Grand Inquisitor wore Wumpus fur",
	"Pain and suffering are always inevitable. Ask Agent %c",
	// Bradbury.
	"Something wicked this way came. Agent %c blinked",
	// Kafka.
	"Agent %c woke up transformed. Into deceased",
	// Dr. Strangelove.
	"Agent %c gave the Wumpus an attack profile. Wumpus accepted",
	"Agent %c: 'we'll meet again' — said the Wumpus",
}

var deathByTTL = []string{
	// Originals.
	"Agent %c died of TTL",
	"Agent %c took the scenic route. Forever",
	"Agent %c wandered too long. Time's up",
	"Agent %c starved. TTL is a harsh mistress",
	"Agent %c ran out the clock. Tragic",
	"Agent %c died. Darwin always wins",
	"Agent %c got lost on the oregon trail",
	"Agent %c died of typhus on the oregon trail.",
	"Agent %c executed halt and catch fire!",
	"Agent %c died...should have chosen a good game of chess",
	"Fast Agent %c pulled an Enron...and ran out of TTL",
	"Agent %c found the stairway to heaven",
	"Agent %c: Have you seen my stapler?",
	"Agent %c pulled the pin...and lost count.",
	"Agent %c fought the law...and the law won.",
	"Agent %c stood when he should have sat down",
	"We're allowed one fatal mistake. Agent %c made it",
	// Dr. Strangelove.
	"Agent %c could not stop worrying. The bomb won",
	"Agent %c precious bodily fluids ran out of TTL",
	"Agent %c rode the bomb. Bomb finished first",
	"Agent %c: 'gentlemen, you can't fight in here, this is the war room!' — TTL",
	// Office Space.
	"Looks like Agent %c is going to need to come in on Saturday",
	"PC LOAD LETTER. Agent %c never loaded. Or letter'd",
	"Bob: We fixed the glitch, Agent %c",
	// War Games.
	"The only winning move is not to play long. Agent %c missed the memo",
	"Shall we play a game? Agent %c said yes. Then expired",
	// Silicon Valley.
	"Agent %c's TAM was infinite. Their TTL was not",
	"Agent %c pivoted to TTL expiration. Investors unmoved",
	// Solzhenitsyn.
	"Agent %c served the day. The day did not serve back",
	"Days of Ivan Denisovich, but for Agent %c",
	// Shakespeare.
	"Tomorrow, and tomorrow, and tomorrow. Then Agent %c stopped",
	// Camus.
	"One must imagine Sisyphus happy. Agent %c was not",
	"Agent %c rolled the boulder. The boulder rolled back",
	// Dostoyevsky.
	"Agent %c suffered, therefore was. Now isn't",
}

var deathByFire = []string{
	// Originals.
	"Agent %c fell into a fire pit",
	"Agent %c walked into a pit. Bottomless",
	"Agent %c walked into a pit. BBQ",
	"Agent %c discovered gravity. The hard way",
	"Agent %c's last words: 'is that smoke?'",
	// Office Space.
	"Agent %c took the printer outside. Forgot the pit",
	// War Games.
	"How about a nice game of fire? Agent %c said yes",
	// Silicon Valley.
	"Agent %c achieved middle-out compression. Into a pit",
	// Bradbury.
	"Agent %c learned what burns at 451°F. Everything",
	"The salamander chose Agent %c",
	// Shakespeare.
	"Out, brief candle! Agent %c was the candle",
	// Dr. Strangelove.
	"Agent %c rode the bomb. The bomb was a fire pit",
}

var deathByOther = []string{
	// Originals.
	"Agent %c died (%s)",
	"Agent %c expired (%s). Inconvenient",
	"Agent %c is no more (%s)",
	// Office Space.
	"Agent %c has a case of the Mondays (%s)",
	// Silicon Valley.
	"Agent %c pivoted to mortality (%s). Series A declined",
	// Shakespeare.
	"Agent %c shuffled off this mortal coil (%s)",
	// Kafka.
	"Agent %c never reached the castle (%s)",
}

// wumpusKilled fires when an agent wins a combat exchange against
// a Wumpus. Rendered yellow — it's a positive moment but not a
// goal reach, and yellow keeps it visually distinct from the
// green goal events.
var wumpusKilled = []string{
	"Agent %c killed a Wumpus",
	"Agent %c: I love the smell of dead wumpus in the morning!",
	"Agent %c notched a Wumpus pelt",
	"Agent %c bagged a Wumpus. The rug awaits",
	"One less Wumpus thanks to Agent %c",
	"Agent %c struck first. Wumpus regrets every life choice",
	"Agent %c: 'this is for my pack'",
	"Wumpus down. Agent %c collects bounty",
	"Agent %c: No fighting in the war room, Wumpus.",
	"Agent %c: Wumpus, you'll have to answer to the coca cola company.",
}

// recordAgentWumpusKill formats a wumpus-kill event using a snark
// template. Called from MoveAgents whenever an agent wins a combat
// exchange.
func (w *World) recordAgentWumpusKill(a *Agent) {
	w.RecordEvent("yellow", fmt.Sprintf(w.pickTemplate(wumpusKilled), a.Label))
}

var goalReached = []string{
	// Originals.
	"Agent %c reached goal",
	"Agent %c found the gold. Show-off",
	"Agent %c made it. Finally",
	"Agent %c stumbled into the goal. We'll allow it",
	"Agent %c: 1, Maze: 0",
	"Agent %c vini vidi vici",
	"Agent %c discovered CPE-1704-TKS and launched the missles",
	"Agent %c hacked the gibson!",
	"Agent %c abides",
	// Office Space.
	"Yeah, Agent %c is gonna need to come in Saturday. To pick up the gold",
	"Agent %c finally got the memo. It said 'win'",
	// War Games.
	"GREETINGS PROFESSOR FALKEN. Agent %c reached the goal",
	"A strange game. Agent %c found the winning move",
	// Silicon Valley.
	"Agent %c shipped MVP. Goal achieved",
	"Make the world a better place. Agent %c just did",
	"Agent %c pivoted to victory. Series B incoming",
	// Shakespeare.
	"All's well that ends well. Agent %c reached the goal",
	"Agent %c — to be! And reach the goal!",
	// Solzhenitsyn.
	"Agent %c's day in the maze. A good one",
	// Hemingway.
	"Agent %c stood, simply, at the goal",
	// Bradbury.
	"Agent %c saw the dandelion wine. Then the gold",
	// Dr. Strangelove.
	"Agent %c learned to stop worrying and love the gold",
}

// pickTemplate returns one entry from `pool`, indexed by World.Rng
// so the choice is deterministic given the world's seed. A single
// rng draw per event keeps the cost negligible.
func (w *World) pickTemplate(pool []string) string {
	if len(pool) == 0 {
		return ""
	}
	return pool[w.Rng.Intn(len(pool))]
}

// recordAgentDeath formats a death event using a reason-specific
// snark template. Called from KillAgent.
func (w *World) recordAgentDeath(a *Agent, reason string) {
	switch reason {
	case "wumpus":
		w.RecordEvent("red", fmt.Sprintf(w.pickTemplate(deathByWumpus), a.Label))
	case "ttl":
		w.RecordEvent("red", fmt.Sprintf(w.pickTemplate(deathByTTL), a.Label))
	case "firepit", "fire-pit", "fire":
		w.RecordEvent("red", fmt.Sprintf(w.pickTemplate(deathByFire), a.Label))
	default:
		w.RecordEvent("red", fmt.Sprintf(w.pickTemplate(deathByOther), a.Label, reason))
	}
}

// recordAgentGoal formats a goal-reach event using a snark template.
// Called from CheckGoal.
func (w *World) recordAgentGoal(a *Agent) {
	w.RecordEvent("green", fmt.Sprintf(w.pickTemplate(goalReached), a.Label))
}

// VisibleEvents returns the last EventsVisible entries (or fewer
// when the buffer hasn't filled). Caller-friendly: the slice is a
// reference into w.Events but the caller shouldn't mutate it.
func (w *World) VisibleEvents() []Event {
	if len(w.Events) <= EventsVisible {
		return w.Events
	}
	return w.Events[len(w.Events)-EventsVisible:]
}
