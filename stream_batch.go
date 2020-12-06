package scylla_cdc

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/gocql/gocql"
)

type streamBatchReader struct {
	config       *ReaderConfig
	streams      []StreamID
	keyspaceName string
	tableName    string

	lastTimestamp gocql.UUID
	endTimestamp  atomic.Value

	interruptCh chan struct{}
}

func newStreamBatchReader(
	config *ReaderConfig,
	streams []StreamID,
	keyspaceName string,
	tableName string,
	startFrom gocql.UUID,
) *streamBatchReader {
	return &streamBatchReader{
		config:       config,
		streams:      streams,
		keyspaceName: keyspaceName,
		tableName:    tableName,

		lastTimestamp: startFrom,

		interruptCh: make(chan struct{}, 1),
	}
}

func (sbr *streamBatchReader) run(ctx context.Context) (gocql.UUID, error) {
	baseTableName := sbr.keyspaceName + "." + sbr.tableName

	input := CreateChangeConsumerInput{
		TableName: baseTableName,
		StreamIDs: sbr.streams,
	}
	consumer, err := sbr.config.ChangeConsumerFactory.CreateChangeConsumer(input)
	if err != nil {
		sbr.config.Logger.Printf("error while creating change consumer (will quit): %s", err)
	}
	defer consumer.End()

	crq := newChangeRowQuerier(sbr.config.Session, sbr.streams, sbr.keyspaceName, sbr.tableName)

	// sbr.config.Logger.Printf("starting stream processor loop for %v", sbr.streams)
outer:
	for {
		timeWindowEnd := sbr.lastTimestamp.Time().Add(sbr.config.Advanced.QueryTimeWindowSize)
		confidenceWindowEnd := time.Now().Add(-sbr.config.Advanced.ConfidenceWindowSize)

		if timeWindowEnd.After(confidenceWindowEnd) {
			timeWindowEnd = confidenceWindowEnd
		}

		pollEnd := gocql.MinTimeUUID(timeWindowEnd)

		var (
			err      error
			rowCount int
		)

		readUpTo := pollEnd

		if CompareTimeuuid(sbr.lastTimestamp, pollEnd) < 0 {
			// Set the time interval from which we need to return data
			var iter *changeRowIterator
			iter, err = crq.queryRange(sbr.lastTimestamp, pollEnd)
			if err != nil {
				sbr.config.Logger.Printf("error while sending a query (will retry): %s", err)
			} else {
				var change Change
				for {
					streamCols, c := iter.Next()
					if c == nil {
						break
					}

					if c.GetOperation() == PreImage {
						change.Preimage = append(change.Preimage, c)
					} else if c.GetOperation() == PostImage {
						change.Postimage = append(change.Postimage, c)
					} else {
						change.Delta = append(change.Delta, c)
					}

					if c.cdcCols.endOfBatch {
						change.StreamID = streamCols.streamID
						change.Time = streamCols.time
						if err := consumer.Consume(change); err != nil {
							// TODO: Does that make sense?
							sbr.config.Logger.Printf("error while processing change (will quit): %s", err)
							return sbr.lastTimestamp, err
						}

						change.Preimage = nil
						change.Delta = nil
						change.Postimage = nil

						// Update the last timestamp only after we processed whole batch
						if CompareTimeuuid(sbr.lastTimestamp, streamCols.time) < 0 {
							sbr.lastTimestamp = streamCols.time
						}
					}

					rowCount++
				}

				if err = iter.Close(); err != nil {
					sbr.config.Logger.Printf("error while querying (will retry): %s", err)
				}
			}
		} else {
			// sbr.config.Logger.Printf("not polling")
			readUpTo = sbr.lastTimestamp
		}

		sbr.lastTimestamp = readUpTo

		if sbr.reachedEndOfTheGeneration(readUpTo) {
			break outer
		}

		var delay time.Duration
		if err != nil {
			delay = sbr.config.Advanced.PostFailedQueryDelay
		} else if rowCount > 0 {
			delay = sbr.config.Advanced.PostNonEmptyQueryDelay
		} else {
			delay = sbr.config.Advanced.PostEmptyQueryDelay
		}

		delayUntil := time.Now().Add(delay)

	delay:
		for {
			select {
			case <-ctx.Done():
				return sbr.lastTimestamp, ctx.Err()
			case <-time.After(delayUntil.Sub(time.Now())):
				break delay
			case <-sbr.interruptCh:
				if sbr.reachedEndOfTheGeneration(readUpTo) {
					break outer
				}
			}
		}

	}
	// sbr.config.Logger.Printf("successfully finishing stream processor loop for %v", sbr.streams)
	return sbr.lastTimestamp, nil
}

func (sbr *streamBatchReader) reachedEndOfTheGeneration(windowEnd gocql.UUID) bool {
	end, isClosed := sbr.endTimestamp.Load().(gocql.UUID)
	return isClosed && (end == gocql.UUID{} || CompareTimeuuid(end, windowEnd) <= 0)
}

// Only one of `close`, `stopNow` methods should be called, only once

func (sbr *streamBatchReader) close(processUntil gocql.UUID) {
	sbr.endTimestamp.Store(processUntil)
	sbr.interruptCh <- struct{}{}
}

func (sbr *streamBatchReader) stopNow() {
	sbr.close(gocql.UUID{})
}
