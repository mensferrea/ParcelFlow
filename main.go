package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"ParcelFlow/internal/auth"
	"ParcelFlow/internal/domain"
	"ParcelFlow/internal/notification"
	"ParcelFlow/internal/outbox"
	paymentclient "ParcelFlow/internal/payment/client"
	"ParcelFlow/internal/payment/simulator"
	pricingclient "ParcelFlow/internal/pricing/client"
	pricinggrpc "ParcelFlow/internal/pricing/delivery/grpc"
	pricingsvc "ParcelFlow/internal/pricing/service"
	shipmenthttp "ParcelFlow/internal/shipment/delivery/http"
	"ParcelFlow/internal/shipment/repository"
	shipmentsvc "ParcelFlow/internal/shipment/service"
	"ParcelFlow/pkg/httphelper"
	"ParcelFlow/pkg/logger"
	"ParcelFlow/pkg/nats"
	"ParcelFlow/pkg/postgres"
	"ParcelFlow/pkg/redis"
	pricingv1 "ParcelFlow/proto/pricing/v1"
)

//go:embed web/*
var webFS embed.FS

func main() {
	log := logger.New("parcelflow")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pgHost := getEnv("DB_HOST", "localhost")
	pgPort := getEnv("DB_PORT", "5432")
	pgUser := getEnv("DB_USER", "postgres")
	pgPass := getEnv("DB_PASSWORD", "postgres")
	pgName := getEnv("DB_NAME", "parcelflow")
	pgSSL := getEnv("DB_SSLMODE", "disable")

	db, err := postgres.New(ctx, postgres.Config{
		Host:     pgHost,
		Port:     pgPort,
		User:     pgUser,
		Password: pgPass,
		DBName:   pgName,
		SSLMode:  pgSSL,
	})
	if err != nil {
		log.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	migrationFile := getEnv("MIGRATION_FILE", "migrations/000001_init_schema.up.sql")
	if err := postgres.RunMigrations(db, migrationFile); err != nil {
		log.Error("failed to run migrations", "error", err)
	}

	redisAddr := getEnv("REDIS_ADDR", "localhost:6379")
	redisClient, err := redis.New(ctx, redisAddr, "", 0)
	if err != nil {
		log.Warn("redis unavailable, running without cache", "error", err)
	} else {
		defer redisClient.Close()
	}

	natsURL := getEnv("NATS_URL", "nats://localhost:4222")
	natsClient, err := nats.New(natsURL)
	if err != nil {
		log.Warn("nats unavailable, outbox events will accumulate in DB", "error", err)
	} else {
		defer natsClient.Close()
	}

	pricingPort := getEnv("PRICING_PORT", "50051")
	pricingListener, err := net.Listen("tcp", fmt.Sprintf(":%s", pricingPort))
	if err != nil {
		log.Error("failed to listen for gRPC pricing", "port", pricingPort, "error", err)
		os.Exit(1)
	}

	pricingService := pricingsvc.NewPricingService()
	grpcHandler := pricinggrpc.NewServer(pricingService)
	grpcServer := grpc.NewServer()
	pricingv1.RegisterPricingServiceServer(grpcServer, grpcHandler)
	reflection.Register(grpcServer)

	go func() {
		log.Info("pricing gRPC service started", "port", pricingPort)
		if err := grpcServer.Serve(pricingListener); err != nil && !errors.Is(err, net.ErrClosed) {
			log.Error("gRPC server error", "error", err)
		}
	}()
	defer grpcServer.GracefulStop()

	pricingClient, err := pricingclient.NewPricingClient(fmt.Sprintf("localhost:%s", pricingPort))
	if err != nil {
		log.Error("failed to create pricing gRPC client", "error", err)
		os.Exit(1)
	}
	defer pricingClient.Close()

	paymentPort := getEnv("PAYMENT_SIMULATOR_PORT", "8082")
	paymentSimulator := simulator.NewSimulator()
	paymentServer := &http.Server{
		Addr:         fmt.Sprintf(":%s", paymentPort),
		Handler:      paymentSimulator.Routes(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		log.Info("payment simulator started", "port", paymentPort)
		if err := paymentServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("payment simulator error", "error", err)
		}
	}()
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer shutdownCancel()
		_ = paymentServer.Shutdown(shutdownCtx)
	}()

	paymentClient := paymentclient.NewPaymentClient(fmt.Sprintf("http://localhost:%s", paymentPort))

	appBaseURL := getEnv("APP_BASE_URL", "http://localhost:8080")
	repo := repository.NewRepository(db)
	shipmentService := shipmentsvc.NewShipmentService(repo, pricingClient, paymentClient, redisClient, appBaseURL)

	jwtSecret := getEnv("JWT_SECRET", "super-secret-key-parcelflow-2026")
	tokenManager := auth.NewTokenManager(jwtSecret, 24*time.Hour)
	seedDefaultUsers(repo)

	if natsClient != nil {
		outboxPublisher := outbox.NewPublisher(repo, natsClient, log)
		outboxPublisher.Start(ctx, 2*time.Second)
		defer outboxPublisher.Stop()

		notificationWorker := notification.NewWorker(repo, natsClient, log)
		if err := notificationWorker.Start(ctx); err != nil {
			log.Error("failed to start notification worker", "error", err)
		} else {
			defer notificationWorker.Stop()
		}
	}

	shipmentHandler := shipmenthttp.NewHandler(shipmentService, tokenManager, redisClient)
	rootMux := http.NewServeMux()

	rootMux.HandleFunc("POST /api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := httphelper.ReadJSON(r, &req); err != nil {
			httphelper.WriteError(w, http.StatusBadRequest, "invalid request")
			return
		}
		user, err := repo.GetUserByEmail(r.Context(), req.Email)
		if err != nil || !auth.CheckPassword(req.Password, user.PasswordHash) {
			httphelper.WriteError(w, http.StatusUnauthorized, "invalid credentials")
			return
		}
		token, err := tokenManager.GenerateToken(user)
		if err != nil {
			httphelper.WriteError(w, http.StatusInternalServerError, "failed to generate token")
			return
		}
		httphelper.WriteJSON(w, http.StatusOK, map[string]any{
			"token": token,
			"user":  user,
		})
	})

	rootMux.Handle("/api/v1/", shipmentHandler.Routes())

	webSub, err := fs.Sub(webFS, "web")
	if err == nil {
		fileServer := http.FileServer(http.FS(webSub))
		rootMux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
			fileServer.ServeHTTP(w, r)
		})
		rootMux.Handle("/ui/", http.StripPrefix("/ui/", fileServer))
	}

	httpPort := getEnv("PORT", "8080")
	apiServer := &http.Server{
		Addr:         fmt.Sprintf(":%s", httpPort),
		Handler:      rootMux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	go func() {
		log.Info("shipment HTTP API and UI started", "port", httpPort)
		if err := apiServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server error", "error", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Info("shutting down ParcelFlow platform")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	_ = apiServer.Shutdown(shutdownCtx)
}

func seedDefaultUsers(repo *repository.Repository) {
	ctx := context.Background()
	adminHash, _ := auth.HashPassword("operator123")
	customerHash, _ := auth.HashPassword("customer123")

	opID, _ := uuid.Parse("11111111-1111-1111-1111-111111111111")
	custID, _ := uuid.Parse("22222222-2222-2222-2222-222222222222")

	_ = repo.CreateUser(ctx, &domain.User{
		ID:           opID,
		Email:        "operator@parcelflow.com",
		PasswordHash: adminHash,
		Role:         domain.RoleOperator,
		CreatedAt:    time.Now(),
	})

	_ = repo.CreateUser(ctx, &domain.User{
		ID:           custID,
		Email:        "customer@parcelflow.com",
		PasswordHash: customerHash,
		Role:         domain.RoleCustomer,
		CreatedAt:    time.Now(),
	})
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
