// Copyright 2020 gorse Project Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package rank

import (
	"github.com/chewxy/math32"
	log "github.com/sirupsen/logrus"
	"github.com/zhenghaoz/gorse/base"
	"github.com/zhenghaoz/gorse/config"
	"github.com/zhenghaoz/gorse/floats"
	"github.com/zhenghaoz/gorse/model"
	"time"
)

type Score struct {
	RMSE      float32
	Precision float32
}

type FactorizationMachine interface {
	// Predict the rating given by a user (userId) to a item (itemId).
	Predict(userId, itemId string, labels []string) float32
	// InternalPredict
	InternalPredict(x []int) float32
}

type BaseFactorizationMachine struct {
	model.BaseModel
	Index UnifiedIndex
}

func (b *BaseFactorizationMachine) Init(trainSet *Dataset) {
	b.Index = trainSet.UnifiedIndex
}

type FMTask string

const (
	FMClassification FMTask = "c"
	FMRegression     FMTask = "r"
)

type FM struct {
	BaseFactorizationMachine
	// Model parameters
	V         [][]float32
	W         []float32
	B         float32
	MinTarget float32
	MaxTarget float32
	Task      FMTask
	// Hyper parameters
	nFactors   int
	nEpochs    int
	lr         float32
	reg        float32
	initMean   float32
	initStdDev float32
}

func NewFM(task FMTask, params model.Params) *FM {
	fm := new(FM)
	fm.Task = task
	fm.SetParams(params)
	return fm
}

func (fm *FM) SetParams(params model.Params) {
	fm.BaseFactorizationMachine.SetParams(params)
	// Setup hyper-parameters
	fm.nFactors = fm.Params.GetInt(model.NFactors, 128)
	fm.nEpochs = fm.Params.GetInt(model.NEpochs, 20)
	fm.lr = fm.Params.GetFloat32(model.Lr, 0.01)
	fm.reg = fm.Params.GetFloat32(model.Reg, 0.0)
	fm.initMean = fm.Params.GetFloat32(model.InitMean, 0)
	fm.initStdDev = fm.Params.GetFloat32(model.InitStdDev, 0.01)
}

func (fm *FM) Predict(userId, itemId string, labels []string) float32 {
	x := make([]int, 0)
	if userIndex := fm.Index.EncodeUser(userId); userIndex != base.NotId {
		x = append(x, userIndex)
	}
	if itemIndex := fm.Index.EncodeItem(itemId); itemIndex != base.NotId {
		x = append(x, itemIndex)
	}
	for _, label := range labels {
		if labelIndex := fm.Index.EncodeLabel(label); labelIndex != base.NotId {
			x = append(x, labelIndex)
		}
	}
	return fm.InternalPredict(x)
}

func (fm *FM) internalPredict(x []int) float32 {
	// w_0
	pred := fm.B
	// \sum^n_{i=1} w_i x_i
	for _, i := range x {
		pred += fm.W[i]
	}
	// \sum^n_{i=1}\sum^n_{j=i+1} <v_i,v_j> x_i x_j
	sum := float32(0)
	for f := 0; f < fm.nFactors; f++ {
		a, b := float32(0), float32(0)
		for _, i := range x {
			// 1) \sum^n_{i=1} v^2_{i,f} x^2_i
			a += fm.V[i][f]
			// 2) \sum^n_{i=1} v^2_{i,f} x^2_i
			b += fm.V[i][f] * fm.V[i][f]
		}
		// 3) (\sum^n_{i=1} v^2_{i,f} x^2_i)^2 - \sum^n_{i=1} v^2_{i,f} x^2_i
		sum += a*a - b
	}
	pred += sum / 2
	return pred
}

func (fm *FM) InternalPredict(x []int) float32 {
	pred := fm.internalPredict(x)
	switch fm.Task {
	case FMRegression:
		if pred < fm.MinTarget {
			pred = fm.MinTarget
		} else if pred > fm.MaxTarget {
			pred = fm.MaxTarget
		}
	}
	return pred
}

func (fm *FM) Fit(trainSet *Dataset, testSet *Dataset, config *config.FitConfig) Score {
	config = config.LoadDefaultIfNil()
	log.Infof("fit FM with hyper-parameters: "+
		"n_factors = %v, n_epochs = %v, lr = %v, reg = %v, init_mean = %v, init_stddev = %v",
		fm.nFactors, fm.nEpochs, fm.lr, fm.reg, fm.initMean, fm.initStdDev)
	log.Infof("       with option: n_jobs = %v", config.Jobs)
	fm.Init(trainSet)
	temp := base.NewMatrix32(config.Jobs, fm.nFactors)
	vGrad := base.NewMatrix32(config.Jobs, fm.nFactors)
	for epoch := 1; epoch <= fm.nEpochs; epoch++ {
		fitStart := time.Now()
		cost := float32(0)
		_ = base.BatchParallel(trainSet.Count(), config.Jobs, 128, func(workerId, beginJobId, endJobId int) error {
			for i := beginJobId; i < endJobId; i++ {
				labels, target := trainSet.Get(i)
				prediction := fm.internalPredict(labels)
				var grad float32
				switch fm.Task {
				case FMRegression:
					fm.MinTarget = math32.Min(fm.MinTarget, target)
					fm.MaxTarget = math32.Max(fm.MaxTarget, target)
					grad = prediction - target
					cost += grad * grad / 2
				case FMClassification:
					grad = -target * (1 - 1/(1+math32.Exp(-target*prediction)))
				default:
					log.Fatal("FM.Fit: unknown task ", fm.Task)
				}
				// \sum^n_{j=1}v_j,fx_j
				floats.Zero(temp[workerId])
				for _, j := range labels {
					floats.Add(temp[workerId], fm.V[j])
				}
				// Update w_0
				fm.B -= fm.lr * grad
				for _, i := range labels {
					// Update w_i
					fm.W[i] -= fm.lr * grad
					// Update v_{i,f}
					floats.SubTo(temp[workerId], fm.V[i], vGrad[workerId])
					floats.MulConst(vGrad[workerId], grad)
					floats.MulConstAddTo(fm.V[i], fm.reg, vGrad[workerId])
					floats.MulConstAddTo(vGrad[workerId], -fm.lr, fm.V[i])
				}
			}
			return nil
		})
		fitTime := time.Since(fitStart)
		// Cross validation
		if epoch%config.Verbose == 0 {
			switch fm.Task {
			case FMRegression:
				evalStart := time.Now()
				rmse := EvaluateRegression(fm, testSet, trainSet)
				evalTime := time.Since(evalStart)
				log.Infof("epoch %v/%v [fit=%v, eval=%v]: loss=%v, RMSE=%v",
					epoch, fm.nEpochs, fitTime, evalTime, cost, rmse)
			case FMClassification:
				evalStart := time.Now()
				precision := EvaluateClassification(fm, testSet, trainSet)
				evalTime := time.Since(evalStart)
				log.Infof("epoch %v/%v [fit=%v, eval=%v]: loss=%v, Precision=%v",
					epoch, fm.nEpochs, fitTime, evalTime, cost, precision)
			default:
				log.Fatal("FM.Fit: unknown task ", fm.Task)
			}
		}
	}
	switch fm.Task {
	case FMRegression:
		return Score{RMSE: EvaluateRegression(fm, testSet, trainSet)}
	case FMClassification:
		return Score{Precision: EvaluateClassification(fm, testSet, trainSet)}
	default:
		log.Fatal("FM.Fit: unknown task ", fm.Task)
		return Score{}
	}
}

func (fm *FM) Init(trainSet *Dataset) {
	newV := fm.Rng.NormalMatrix(trainSet.UnifiedIndex.Len(), fm.nFactors, fm.initMean, fm.initStdDev)
	newW := make([]float32, trainSet.UnifiedIndex.Len())
	// Relocate parameters
	if fm.Index != nil {
		// users
		for _, userId := range trainSet.UnifiedIndex.GetUsers() {
			oldIndex := fm.Index.EncodeUser(userId)
			newIndex := trainSet.UnifiedIndex.EncodeUser(userId)
			if oldIndex != base.NotId {
				newW[newIndex] = fm.W[oldIndex]
				newV[newIndex] = fm.V[oldIndex]
			}
		}
		// items
		for _, itemId := range trainSet.UnifiedIndex.GetItems() {
			oldIndex := fm.Index.EncodeItem(itemId)
			newIndex := trainSet.UnifiedIndex.EncodeItem(itemId)
			if oldIndex != base.NotId {
				newW[newIndex] = fm.W[oldIndex]
				newV[newIndex] = fm.V[oldIndex]
			}
		}
		// labels
		for _, label := range trainSet.UnifiedIndex.GetLabels() {
			oldIndex := fm.Index.EncodeLabel(label)
			newIndex := trainSet.UnifiedIndex.EncodeLabel(label)
			if oldIndex != base.NotId {
				newW[newIndex] = fm.W[oldIndex]
				newV[newIndex] = fm.V[oldIndex]
			}
		}
	}
	fm.MinTarget = math32.Inf(1)
	fm.MaxTarget = math32.Inf(-1)
	fm.V = newV
	fm.W = newW
	fm.BaseFactorizationMachine.Init(trainSet)
}