/*
Copyright 2024 The Aibrix Team.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package utils

import (
	"context"

	"github.com/redis/go-redis/v9"

	"k8s.io/klog/v2"
)

func GetRedisClient() *redis.Client {
	redisHost := LoadEnv("REDIS_HOST", "localhost")
	redisPort := LoadEnv("REDIS_PORT", "6379")
	redisPassword := LoadEnv("REDIS_PASSWORD", "")
	// Connect to Redis
	client := redis.NewClient(&redis.Options{
		Addr:     redisHost + ":" + redisPort,
		Password: redisPassword,
		DB:       0, // Default DB
	})
	pong, err := client.Ping(context.Background()).Result()
	if err != nil {
		klog.Fatalf("Error connecting to Redis: %v", err)
	}
	klog.Infof("Connected to Redis: %s", pong)
	return client
}
